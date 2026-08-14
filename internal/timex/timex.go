// Package timex centralizes the timestamp encoding used in the database.
//
// SQLite has no native timestamp type, so every stored time is an ISO8601 UTC
// string with millisecond precision. Keeping that format in one place is what
// makes the eventual move to Postgres TIMESTAMPTZ a column-type change rather
// than an audit of every query.
package timex

import "time"

// Layout matches the strftime('%Y-%m-%dT%H:%M:%fZ') default in the migrations.
const Layout = "2006-01-02T15:04:05.000Z"

// Format renders t as a UTC ISO8601 string.
func Format(t time.Time) string {
	return t.UTC().Format(Layout)
}

// Now is the current time, formatted for storage.
func Now() string {
	return Format(time.Now())
}

// Parse reads a stored timestamp. It accepts RFC3339 as a fallback so rows
// written by other tools (or a future Postgres backend) still parse.
func Parse(s string) (time.Time, error) {
	if t, err := time.Parse(Layout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// Expired reports whether the stored timestamp is in the past. An unparseable
// value is treated as expired so a corrupt row fails closed.
func Expired(s string) bool {
	t, err := Parse(s)
	if err != nil {
		return true
	}
	return time.Now().UTC().After(t)
}
