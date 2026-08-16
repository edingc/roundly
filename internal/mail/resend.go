package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	resendEndpoint = "https://api.resend.com/emails"
	resendTimeout  = 15 * time.Second
)

// resendMailer posts to Resend's HTTP API.
//
// Worth having alongside SMTP for one practical reason: a great many hosts
// block outbound 587 and 465, and on those the SMTP path cannot be made to work
// at all. HTTPS is never blocked.
type resendMailer struct {
	apiKey   string
	from     string
	endpoint string
	client   *http.Client
}

func newResend(apiKey, from string) Mailer {
	return &resendMailer{
		apiKey:   apiKey,
		from:     from,
		endpoint: resendEndpoint,
		client:   &http.Client{Timeout: resendTimeout},
	}
}

func (m *resendMailer) Describe() string { return "Resend (https://api.resend.com)" }

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
}

func (m *resendMailer) Send(ctx context.Context, msg Message) error {
	if err := validRecipient(msg.To); err != nil {
		return err
	}

	body, err := json.Marshal(resendRequest{
		From:    m.from,
		To:      []string{msg.To},
		Subject: sanitizeSubject(msg.Subject),
		Text:    msg.Text,
		HTML:    msg.HTML,
	})
	if err != nil {
		return fmt.Errorf("mail: encode resend request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mail: build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: resend request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Resend explains rejections in the body — a domain that is not verified,
	// a From that is not on it — and those are exactly the errors an operator
	// needs to read. Capped so a misbehaving endpoint cannot fill the log.
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("mail: resend returned %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
}
