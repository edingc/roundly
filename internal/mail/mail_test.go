package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The selection rule is the whole of the configuration contract, and getting it
// wrong means an instance that silently cannot send.
func TestNewSelectsATransport(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		want    string // substring of Describe, or "" for no mailer
		wantErr bool
	}{
		{
			name: "nothing configured is not an error",
			cfg:  Config{},
		},
		{
			name: "smtp",
			cfg:  Config{From: "Roundly <no-reply@example.com>", SMTPHost: "smtp.example.com"},
			want: "SMTP smtp.example.com:587",
		},
		{
			name: "resend",
			cfg:  Config{From: "Roundly <no-reply@example.com>", ResendAPIKey: "re_test"},
			want: "Resend",
		},
		{
			// An operator who set an API key chose it. Preferring the other one
			// because it also happened to be set would be the wrong reading.
			name: "resend wins when both are set",
			cfg: Config{
				From:         "Roundly <no-reply@example.com>",
				ResendAPIKey: "re_test",
				SMTPHost:     "smtp.example.com",
			},
			want: "Resend",
		},
		{
			name:    "a transport with no from address",
			cfg:     Config{SMTPHost: "smtp.example.com"},
			wantErr: true,
		},
		{
			name:    "a from address with no transport",
			cfg:     Config{From: "Roundly <no-reply@example.com>"},
			wantErr: true,
		},
		{
			name:    "an unparseable from address",
			cfg:     Config{From: "not an address", SMTPHost: "smtp.example.com"},
			wantErr: true,
		},
		{
			name: "smtp credentials half filled in",
			cfg: Config{
				From:         "Roundly <no-reply@example.com>",
				SMTPHost:     "smtp.example.com",
				SMTPUsername: "user",
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mailer, err := New(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("err = nil, want the misconfiguration reported")
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if tc.want == "" {
				if mailer != nil {
					t.Errorf("mailer = %s, want nil", mailer.Describe())
				}
				return
			}
			if mailer == nil {
				t.Fatalf("mailer = nil, want %s", tc.want)
			}
			if got := mailer.Describe(); !strings.Contains(got, tc.want) {
				t.Errorf("Describe = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestBuildMIMECarriesBothBodies(t *testing.T) {
	raw := string(buildMIME("Roundly <no-reply@example.com>", Message{
		To:      "golfer@example.com",
		Subject: "Your Roundly sign-in code",
		Text:    "plain body",
		HTML:    "<p>html body</p>",
	}))

	for _, want := range []string{
		"From: Roundly <no-reply@example.com>",
		"To: golfer@example.com",
		"MIME-Version: 1.0",
		"multipart/alternative",
		"text/plain; charset=utf-8",
		"text/html; charset=utf-8",
		"plain body",
		"<p>html body</p>",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("message is missing %q:\n%s", want, raw)
		}
	}

	// HTML has to come second: a multipart/alternative reader takes the last
	// part it understands.
	if strings.Index(raw, "text/plain") > strings.Index(raw, "text/html") {
		t.Error("the HTML part comes before the plain-text one, so clients will prefer plain text")
	}
	// Every line break in a message has to be CRLF, or the headers run together.
	if strings.Contains(strings.ReplaceAll(raw, "\r\n", ""), "\n") {
		t.Error("the message contains a bare LF, want CRLF throughout")
	}
}

// A lone "." on its own line ends the SMTP DATA command, so a body containing
// one would be truncated there.
func TestBuildMIMEEscapesALeadingDot(t *testing.T) {
	raw := string(buildMIME("no-reply@example.com", Message{
		To:   "golfer@example.com",
		Text: "before\n.\nafter",
		HTML: "<p>x</p>",
	}))
	if !strings.Contains(raw, "\r\n..\r\n") {
		t.Errorf("a bare dot line was not stuffed:\n%s", raw)
	}
}

// A recipient is interpolated into a protocol where a newline starts a new
// command, so it is checked before it gets anywhere near one.
func TestSendRejectsAnInjectedRecipient(t *testing.T) {
	mailer := newResend("re_test", "no-reply@example.com")
	for name, to := range map[string]string{
		"empty":          "",
		"header break":   "golfer@example.com\r\nBcc: victim@example.com",
		"not an address": "not an address",
	} {
		t.Run(name, func(t *testing.T) {
			if err := mailer.Send(context.Background(), Message{To: to}); err == nil {
				t.Error("err = nil, want the recipient rejected")
			}
		})
	}
}

func TestResendPostsTheMessage(t *testing.T) {
	var got resendRequest
	var auth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer server.Close()

	mailer := newResend("re_test", "Roundly <no-reply@example.com>").(*resendMailer)
	mailer.endpoint = server.URL

	err := mailer.Send(context.Background(), Message{
		To:      "golfer@example.com",
		Subject: "Your Roundly sign-in code",
		Text:    "123456",
		HTML:    "<p>123456</p>",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if auth != "Bearer re_test" {
		t.Errorf("Authorization = %q, want the API key as a bearer token", auth)
	}
	if got.From != "Roundly <no-reply@example.com>" {
		t.Errorf("from = %q, want the configured sender", got.From)
	}
	if len(got.To) != 1 || got.To[0] != "golfer@example.com" {
		t.Errorf("to = %v, want the one recipient", got.To)
	}
	if got.Text == "" || got.HTML == "" {
		t.Error("both bodies should be sent, so clients that block HTML still get the code")
	}
}

// Resend explains a rejection in the body — an unverified domain, a From that
// is not on it — and those are exactly what an operator needs to read.
func TestResendReportsWhatWentWrong(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"The example.com domain is not verified"}`))
	}))
	defer server.Close()

	mailer := newResend("re_test", "no-reply@example.com").(*resendMailer)
	mailer.endpoint = server.URL

	err := mailer.Send(context.Background(), Message{To: "golfer@example.com", Subject: "x"})
	if err == nil {
		t.Fatal("err = nil, want the rejection reported")
	}
	if !strings.Contains(err.Error(), "not verified") {
		t.Errorf("err = %v, want it to carry what the API said", err)
	}
}

func TestEnvelopeAddressStripsTheDisplayName(t *testing.T) {
	for input, want := range map[string]string{
		"Roundly <no-reply@example.com>": "no-reply@example.com",
		"no-reply@example.com":           "no-reply@example.com",
		"  spaced@example.com  ":         "spaced@example.com",
	} {
		if got := envelopeAddress(input); got != want {
			t.Errorf("envelopeAddress(%q) = %q, want %q", input, got, want)
		}
	}
}
