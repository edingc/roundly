// Package id generates primary keys.
//
// IDs are UUIDv7 strings generated in the application, never by the database.
// v7 is time-ordered, so keys cluster by creation time and index locality holds
// up as tables grow; generating them here keeps the schema free of SQLite- or
// Postgres-specific ID functions.
package id

import "github.com/google/uuid"

// New returns a UUIDv7 string.
func New() string {
	v7, err := uuid.NewV7()
	if err != nil {
		// NewV7 only fails when the system entropy source fails, in which case
		// v4 is an equally valid (just unordered) fallback.
		return uuid.NewString()
	}
	return v7.String()
}
