package httpx

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Validator accumulates field errors so a request reports every problem at once
// rather than failing on the first one.
type Validator struct {
	fields map[string]string
}

func NewValidator() *Validator {
	return &Validator{fields: make(map[string]string)}
}

// Add records a message for field unless that field already has one, keeping
// the first (most specific) failure per field.
func (v *Validator) Add(field, message string) {
	if _, exists := v.fields[field]; !exists {
		v.fields[field] = message
	}
}

func (v *Validator) Check(ok bool, field, message string) {
	if !ok {
		v.Add(field, message)
	}
}

// Required trims the value and reports whether it is non-empty.
func (v *Validator) Required(field, value string) bool {
	if strings.TrimSpace(value) == "" {
		v.Add(field, "This field is required.")
		return false
	}
	return true
}

// MaxLen bounds a string by rune count.
func (v *Validator) MaxLen(field, value string, max int) {
	if utf8.RuneCountInString(value) > max {
		v.Add(field, fmt.Sprintf("Must be %d characters or fewer.", max))
	}
}

// IntBetween bounds an integer inclusively.
func (v *Validator) IntBetween(field string, value, min, max int) {
	if value < min || value > max {
		v.Add(field, fmt.Sprintf("Must be between %d and %d.", min, max))
	}
}

// Email validates an address and returns it normalized to lowercase.
func (v *Validator) Email(field, value string) string {
	normalized := NormalizeEmail(value)
	if normalized == "" {
		v.Add(field, "This field is required.")
		return ""
	}
	addr, err := mail.ParseAddress(normalized)
	if err != nil || addr.Address != normalized {
		v.Add(field, "Enter a valid email address.")
		return normalized
	}
	v.MaxLen(field, normalized, 254)
	return normalized
}

// HexColor validates a #RGB or #RRGGBB color and returns it uppercased.
func (v *Validator) HexColor(field, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		v.Add(field, "Pick a color.")
		return ""
	}
	if !strings.HasPrefix(trimmed, "#") || (len(trimmed) != 4 && len(trimmed) != 7) {
		v.Add(field, "Use a hex color such as #FFD700.")
		return trimmed
	}
	for _, c := range trimmed[1:] {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			v.Add(field, "Use a hex color such as #FFD700.")
			return trimmed
		}
	}
	return strings.ToUpper(trimmed)
}

func (v *Validator) Valid() bool { return len(v.fields) == 0 }

// Err returns a validation APIError, or nil when everything passed.
func (v *Validator) Err() error {
	if v.Valid() {
		return nil
	}
	return ValidationError(v.fields)
}

// NormalizeEmail lowercases and trims an address so that lookups and the users
// UNIQUE constraint agree on what counts as the same account.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
