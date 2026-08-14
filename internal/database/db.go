// Package database opens the SQLite database, applies migrations, and exposes
// the sqlc-generated query layer.
package database

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/edingc/roundly/db"
	"github.com/edingc/roundly/internal/database/sqlc"
)

type DB struct {
	*sql.DB
	Queries *sqlc.Queries
}

// Open connects to the SQLite file at dsn and applies any pending migrations.
func Open(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", withPragmas(dsn))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite tolerates exactly one writer. WAL plus a single pooled connection
	// keeps writes serialized in-process instead of surfacing SQLITE_BUSY.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, err
	}

	return &DB{DB: conn, Queries: sqlc.New(conn)}, nil
}

// withPragmas appends the connection pragmas modernc/sqlite reads from the DSN.
// Foreign keys are off by default in SQLite and the schema leans on ON DELETE
// CASCADE, so enabling them is required rather than a tuning choice.
func withPragmas(dsn string) string {
	if strings.Contains(dsn, "_pragma=") {
		return dsn
	}
	pragmas := []string{
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + strings.Join(pragmas, "&")
}

func migrate(conn *sql.DB) error {
	goose.SetBaseFS(db.MigrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(conn, db.MigrationsDir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// InTx runs fn inside a transaction, rolling back unless fn returns nil.
func (d *DB) InTx(fn func(*sqlc.Queries) error) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(d.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
