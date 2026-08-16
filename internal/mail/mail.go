// Package mail sends the handful of transactional messages this app needs:
// an address-verification link and a sign-in code.
//
// Two backends, because the two ways people run this differ. Someone
// self-hosting already has a mailbox somewhere and wants to point SMTP at it;
// someone on a platform that blocks outbound port 587 needs an HTTP API, and
// Resend is the one with a free tier that does not require a sales call.
// Neither is a dependency: both are the standard library plus one struct.
//
// A nil Mailer is a valid state and means this instance cannot send mail, which
// is the default. Every caller has to handle it, because every feature built on
// it — verification, two-factor — has to stay switchable off.
package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Message is one email. Both bodies are required: Text for the clients and
// filters that will not render HTML, HTML for everyone else.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Mailer sends a message, or explains why it could not.
//
// Send is expected to be slow — it talks to another host — and callers should
// treat a failure as a failure of the action that triggered it. A verification
// email that silently does not arrive is worse than a signup that reports it
// could not finish.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
	// Describe names the backend for a startup log line, so an operator can see
	// which one their configuration actually selected.
	Describe() string
}

// ErrNotConfigured is what a caller gets from Require when this instance has no
// mailer. It is a plain sentinel rather than an HTTP error so that internal/mail
// need not know about the transport layer.
var ErrNotConfigured = errors.New("mail: no mailer is configured on this server")

// Require turns a possibly-nil Mailer into a usable one or an error, so callers
// can stop repeating the same nil check.
func Require(m Mailer) (Mailer, error) {
	if m == nil {
		return nil, ErrNotConfigured
	}
	return m, nil
}

// Config is the whole of the mail configuration, read from the environment.
type Config struct {
	// From is the envelope and header sender, e.g. `Roundly <no-reply@x.com>`.
	// Required by both backends: a message with no From is a message every
	// receiver rejects.
	From string

	// Resend, when set, wins. An operator who has configured an API key has
	// made a choice, and silently preferring SMTP because it was also set would
	// be the wrong reading of it.
	ResendAPIKey string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
}

// New builds the mailer a configuration selects, or nil when it selects none.
//
// Returning (nil, nil) is deliberate and is the common case: an instance with
// no mail configuration is not misconfigured, it is an instance where the
// features that need mail stay switched off. A configuration that is half
// filled in is a different matter and is an error, because it is always a
// mistake rather than a decision.
func New(cfg Config) (Mailer, error) {
	hasResend := cfg.ResendAPIKey != ""
	hasSMTP := cfg.SMTPHost != ""

	if !hasResend && !hasSMTP {
		if cfg.From != "" {
			return nil, fmt.Errorf("MAIL_FROM is set but no transport is: set RESEND_API_KEY or SMTP_HOST")
		}
		return nil, nil
	}

	if cfg.From == "" {
		return nil, fmt.Errorf("MAIL_FROM must be set to send mail")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("MAIL_FROM is not a valid address: %w", err)
	}

	if hasResend {
		return newResend(cfg.ResendAPIKey, cfg.From), nil
	}
	return newSMTP(cfg)
}

// validRecipient rejects an address before it reaches a transport.
//
// Both backends put the recipient into a protocol where a newline would start a
// new command or a new header, so this is a header-injection check as much as a
// validity one. ParseAddress already refuses embedded newlines; the explicit
// test is here because that is the property being relied on, and a future
// change to the parser should break this rather than open a hole quietly.
func validRecipient(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return errors.New("mail: no recipient")
	}
	if strings.ContainsAny(address, "\r\n") {
		return errors.New("mail: recipient contains a line break")
	}
	if _, err := mail.ParseAddress(address); err != nil {
		return fmt.Errorf("mail: invalid recipient: %w", err)
	}
	return nil
}

// sanitizeSubject strips what would otherwise let a subject line inject
// headers. Subjects here are built from constants, so this is belt and braces.
func sanitizeSubject(subject string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(subject)
}
