// Package db holds the SQL schema assets: goose migrations (embedded here so
// the server ships as a single binary) and the sqlc query sources.
package db

import "embed"

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationsDir is the path within MigrationsFS that goose should walk.
const MigrationsDir = "migrations"
