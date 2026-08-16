package auth

import (
	"fmt"
	"html"
	"strings"

	"github.com/edingc/roundly/internal/mail"
)

// The two messages this app sends.
//
// Written as string building rather than html/template because they are two
// fixed documents with three substitutions between them, and because every
// substitution here is either a URL this server just built or six digits it
// just generated. The one value that could carry anything — the display name —
// is escaped explicitly. A template engine would add a layer without removing
// the need to think about that.
//
// Both are plain text plus a deliberately plain HTML part: no images, no
// tracking pixel, no external stylesheet. A message that renders as an empty
// box when the images are blocked is a message that gets reported as phishing.

// mailStyles keeps the HTML legible in clients that ignore most CSS. Inline
// because that is the only thing every mail client honours.
const (
	bodyStyle   = "font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; font-size: 16px; line-height: 1.5; color: #0f172a;"
	buttonStyle = "display: inline-block; padding: 12px 20px; background: #16803c; color: #ffffff; text-decoration: none; border-radius: 8px; font-weight: 600;"
	codeStyle   = "display: inline-block; padding: 12px 20px; background: #f1f5f9; border-radius: 8px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 28px; letter-spacing: 6px; font-weight: 700;"
	mutedStyle  = "color: #64748b; font-size: 14px;"
)

// verificationEmail is the link sent when an address is first claimed or
// changed.
func verificationEmail(to, displayName, link string) mail.Message {
	greeting := greet(displayName)
	safeLink := html.EscapeString(link)

	text := strings.Join([]string{
		greeting,
		"",
		"Confirm this address to finish setting up your Roundly account:",
		"",
		link,
		"",
		"The link is good for 24 hours and can be used once.",
		"",
		"If you did not create a Roundly account, you can ignore this message — nothing will happen until the link is opened.",
	}, "\n")

	htmlBody := fmt.Sprintf(`<div style="%s">
  <p>%s</p>
  <p>Confirm this address to finish setting up your Roundly account.</p>
  <p><a href="%s" style="%s">Confirm my email</a></p>
  <p style="%s">The link is good for 24 hours and can be used once. If the button does not work, paste this into your browser:<br><a href="%s">%s</a></p>
  <p style="%s">If you did not create a Roundly account, you can ignore this message — nothing will happen until the link is opened.</p>
</div>`,
		bodyStyle, html.EscapeString(greeting), safeLink, buttonStyle, mutedStyle, safeLink, safeLink, mutedStyle)

	return mail.Message{
		To:      to,
		Subject: "Confirm your Roundly email address",
		Text:    text,
		HTML:    htmlBody,
	}
}

// loginCodeEmail is the second factor: six digits, and a warning.
//
// The warning is the useful half. Somebody receiving this who is not signing in
// has just learned their password is known, and the message has to say so
// plainly enough that they act on it.
func loginCodeEmail(to, displayName, code string) mail.Message {
	greeting := greet(displayName)

	text := strings.Join([]string{
		greeting,
		"",
		"Your Roundly sign-in code is:",
		"",
		"    " + code,
		"",
		"It expires in 10 minutes.",
		"",
		"If you are not signing in right now, someone else has your password. Change it as soon as you can — until then, this code is what is keeping them out.",
	}, "\n")

	htmlBody := fmt.Sprintf(`<div style="%s">
  <p>%s</p>
  <p>Your Roundly sign-in code is:</p>
  <p><span style="%s">%s</span></p>
  <p style="%s">It expires in 10 minutes.</p>
  <p><strong>If you are not signing in right now, someone else has your password.</strong> Change it as soon as you can — until then, this code is what is keeping them out.</p>
</div>`,
		bodyStyle, html.EscapeString(greeting), codeStyle, html.EscapeString(code), mutedStyle)

	return mail.Message{
		To:      to,
		Subject: "Your Roundly sign-in code",
		Text:    text,
		HTML:    htmlBody,
	}
}

// greet addresses the person by name when there is one worth using.
//
// Display names are user-controlled and end up in a subject-adjacent position,
// so this trims, collapses whitespace, and gives up on anything long or empty
// rather than trying to salvage it.
func greet(displayName string) string {
	name := strings.Join(strings.Fields(displayName), " ")
	if name == "" || len(name) > 60 {
		return "Hello,"
	}
	return "Hello " + name + ","
}
