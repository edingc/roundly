package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
)

// smtpStub is just enough of an SMTP server to hold one conversation, so the
// client can be tested without a mail server or a network.
type smtpStub struct {
	listener net.Listener

	mu       sync.Mutex
	commands []string
	data     string
	// offerStartTLS controls the EHLO capability list, which is what decides
	// whether the client is willing to authenticate.
	offerStartTLS bool
}

func newSMTPStub(t *testing.T, offerStartTLS bool) *smtpStub {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stub := &smtpStub{listener: listener, offerStartTLS: offerStartTLS}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go stub.handle(conn)
		}
	}()
	return stub
}

func (s *smtpStub) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 stub ESMTP ready")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimSpace(line)

		s.mu.Lock()
		s.commands = append(s.commands, command)
		s.mu.Unlock()

		upper := strings.ToUpper(command)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			if s.offerStartTLS {
				write("250-stub")
				write("250-STARTTLS")
				write("250 AUTH PLAIN")
			} else {
				write("250-stub")
				write("250 AUTH PLAIN")
			}
		case strings.HasPrefix(upper, "HELO"):
			write("250 stub")
		case strings.HasPrefix(upper, "MAIL FROM"), strings.HasPrefix(upper, "RCPT TO"):
			write("250 ok")
		case strings.HasPrefix(upper, "DATA"):
			write("354 send it")
			var body strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				body.WriteString(dataLine)
			}
			s.mu.Lock()
			s.data = body.String()
			s.mu.Unlock()
			write("250 queued")
		case strings.HasPrefix(upper, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 ok")
		}
	}
}

func (s *smtpStub) address() (string, int) {
	addr := s.listener.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func (s *smtpStub) transcript() ([]string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...), s.data
}

// A local relay with no credentials is the self-hosted case: no TLS to
// negotiate and nothing to protect, so the message just goes.
func TestSMTPSendsThroughALocalRelay(t *testing.T) {
	stub := newSMTPStub(t, false)
	host, port := stub.address()

	mailer, err := New(Config{
		From:     "Roundly <no-reply@example.com>",
		SMTPHost: host,
		SMTPPort: port,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = mailer.Send(context.Background(), Message{
		To:      "golfer@example.com",
		Subject: "Your Roundly sign-in code",
		Text:    "123456",
		HTML:    "<p>123456</p>",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	commands, data := stub.transcript()
	joined := strings.Join(commands, "\n")
	// The envelope takes the bare address; the display name belongs in the
	// header and nowhere else.
	if !strings.Contains(joined, "MAIL FROM:<no-reply@example.com>") {
		t.Errorf("MAIL FROM was not the bare address:\n%s", joined)
	}
	if !strings.Contains(joined, "RCPT TO:<golfer@example.com>") {
		t.Errorf("RCPT TO was not the bare address:\n%s", joined)
	}
	if !strings.Contains(data, "123456") {
		t.Errorf("the code did not reach the body:\n%s", data)
	}
	if !strings.Contains(data, "Subject: ") {
		t.Errorf("no subject header:\n%s", data)
	}
}

// The one refusal worth having: a remote relay that will not offer STARTTLS is
// a relay this client will not hand a password to.
func TestSMTPRefusesToSendCredentialsInTheClear(t *testing.T) {
	stub := newSMTPStub(t, false)
	_, port := stub.address()

	mailer, err := New(Config{
		From:         "Roundly <no-reply@example.com>",
		SMTPHost:     "127.0.0.1",
		SMTPPort:     port,
		SMTPUsername: "user",
		SMTPPassword: "secret",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client := mailer.(*smtpMailer)
	// Loopback is exempt by design — that is the local-postfix case — so the
	// host is renamed to something remote while still dialling the stub.
	client.host = "mail.example.com"
	client.dial = func(ctx context.Context, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", stub.listener.Addr().String())
	}

	err = client.Send(context.Background(), Message{To: "golfer@example.com", Subject: "x", Text: "y"})
	if err == nil {
		t.Fatal("err = nil, want the send refused rather than the password disclosed")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("err = %v, want it to name STARTTLS as the reason", err)
	}

	commands, _ := stub.transcript()
	for _, command := range commands {
		if strings.HasPrefix(strings.ToUpper(command), "AUTH") {
			t.Fatalf("the client authenticated anyway: %q", command)
		}
	}
}
