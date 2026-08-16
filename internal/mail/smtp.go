package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// smtpTimeout bounds the whole conversation. A mail server that has stopped
// answering must not hold a signup request open until the server's own 30s
// timeout fires.
const smtpTimeout = 15 * time.Second

// smtpMailer sends through any SMTP server: a provider's relay, a company
// server, or a local postfix on port 25.
type smtpMailer struct {
	host     string
	port     int
	username string
	password string
	from     string

	// dial is swappable so tests can run against an in-process server.
	dial func(ctx context.Context, address string) (net.Conn, error)
}

func newSMTP(cfg Config) (Mailer, error) {
	port := cfg.SMTPPort
	if port == 0 {
		// 587 (submission with STARTTLS) rather than 25: 25 is for server-to-
		// server relay and is blocked outbound by most networks worth running
		// on, and rather than 465, which is implicit TLS and less widely the
		// default on the providers people actually use.
		port = 587
	}
	if (cfg.SMTPUsername == "") != (cfg.SMTPPassword == "") {
		return nil, errors.New("SMTP_USERNAME and SMTP_PASSWORD must be set together")
	}
	return &smtpMailer{
		host:     cfg.SMTPHost,
		port:     port,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		from:     cfg.From,
		dial: func(ctx context.Context, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", address)
		},
	}, nil
}

func (m *smtpMailer) Describe() string {
	auth := "no authentication"
	if m.username != "" {
		auth = "authenticating as " + m.username
	}
	return fmt.Sprintf("SMTP %s:%d (%s)", m.host, m.port, auth)
}

func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	if err := validRecipient(msg.To); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, smtpTimeout)
	defer cancel()

	address := net.JoinHostPort(m.host, strconv.Itoa(m.port))
	conn, err := m.dial(ctx, address)
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", address, err)
	}
	// Closed unconditionally: Quit closes it on the happy path, and this covers
	// every return before that. A double close is a harmless error we drop.
	defer func() { _ = conn.Close() }()

	// The deadline is what makes ctx mean anything here — net/smtp has no
	// context-aware API, so cancellation has to arrive through the socket.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return fmt.Errorf("mail: smtp handshake with %s: %w", m.host, err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: m.host}); err != nil {
			return fmt.Errorf("mail: starttls with %s: %w", m.host, err)
		}
	} else if m.username != "" && !isLoopback(m.host) {
		// Refusing rather than downgrading. Sending a password in the clear to
		// a remote host is not a degraded mode of this feature, it is a
		// disclosure — and a relay that will not offer STARTTLS in 2026 is
		// misconfigured. A loopback relay is exempt: there is no network to
		// eavesdrop on, and that is the local-postfix case.
		return fmt.Errorf("mail: %s does not offer STARTTLS, refusing to send credentials in the clear", m.host)
	}

	if m.username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
			return fmt.Errorf("mail: authenticate to %s: %w", m.host, err)
		}
	}

	if err := client.Mail(envelopeAddress(m.from)); err != nil {
		return fmt.Errorf("mail: sender rejected by %s: %w", m.host, err)
	}
	if err := client.Rcpt(envelopeAddress(msg.To)); err != nil {
		return fmt.Errorf("mail: recipient rejected by %s: %w", m.host, err)
	}

	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA rejected by %s: %w", m.host, err)
	}
	if _, err := body.Write(buildMIME(m.from, msg)); err != nil {
		return fmt.Errorf("mail: write message: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("mail: message rejected by %s: %w", m.host, err)
	}

	return client.Quit()
}

// envelopeAddress reduces `Name <addr@example.com>` to `addr@example.com`,
// which is the only form the MAIL FROM and RCPT TO commands accept.
func envelopeAddress(address string) string {
	if start := strings.LastIndex(address, "<"); start >= 0 {
		if end := strings.Index(address[start:], ">"); end > 0 {
			return address[start+1 : start+end]
		}
	}
	return strings.TrimSpace(address)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// buildMIME renders a multipart/alternative message: the same content as plain
// text and as HTML, with the client picking.
//
// Written out by hand rather than with a library because it is thirty lines of
// well-specified format, and pulling in a dependency to concatenate strings is
// how a small binary stops being one.
func buildMIME(from string, msg Message) []byte {
	// Fixed rather than random: the boundary only has to not appear in the
	// body, and both bodies here are generated from templates that cannot
	// contain it.
	const boundary = "roundly-boundary-8f2a1c4e6b90"

	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + msg.To + "\r\n")
	// Encoded so a subject is not restricted to ASCII, even though today's
	// never leave it.
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", sanitizeSubject(msg.Subject)) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	// Tells the well-behaved half of the world's autoresponders not to reply to
	// a sign-in code with an out-of-office.
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	// Least-preferred part first: a multipart/alternative reader takes the last
	// part it understands, so HTML has to come second to win.
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(dotStuff(msg.Text))
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
	b.WriteString(dotStuff(msg.HTML))
	b.WriteString("\r\n")

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// dotStuff normalises line endings and escapes a leading dot.
//
// A bare "." on its own line ends the DATA command, so a body containing one
// would truncate the message there — the classic SMTP injection. net/smtp's
// writer does this too, but only for line endings it can see, and doing it here
// keeps the rendered message correct on its own terms.
func dotStuff(body string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = "." + line
		}
	}
	return strings.Join(lines, "\r\n")
}
