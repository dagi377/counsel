// Package migrations embeds the numbered schema migrations of this directory and applies
// them in file-name order. migrations/README.md describes the numbering and the
// expand-contract rules; tools/migration_blocks_test.go enforces them.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"
)

//go:embed *.sql
var files embed.FS

// numbered matches a migration file name; README.md and smoke.sql are not migrations.
var numbered = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)

// Names returns every numbered migration in apply order (file-name order).
func Names() []string {
	entries, _ := fs.ReadDir(files, ".")
	var out []string
	for _, e := range entries {
		if numbered.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Read returns the SQL of the named migration.
func Read(name string) ([]byte, error) {
	b, err := files.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read migration %s: %w", name, err)
	}
	return b, nil
}

// Apply runs every migration whose name sorts at or before upTo (all of them when upTo is
// empty) on conn, in order, and records each in the schema version table. A file already
// recorded is skipped, so Apply can run again on a migrated database. A file runs as one
// simple-protocol batch (one transaction, with its record), so it may hold several statements
// and DO blocks; a file marked "-- migrate: no-transaction" runs statement by statement.
func Apply(ctx context.Context, conn *pgx.Conn, upTo string) error {
	if _, err := conn.PgConn().Exec(ctx, ensureVersionTable).ReadAll(); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}
	done, err := applied(ctx, conn)
	if err != nil {
		return err
	}
	for _, name := range Names() {
		if upTo != "" && name > upTo {
			break
		}
		if done[name] {
			continue
		}
		sql, err := Read(name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, conn, name, string(sql)); err != nil {
			return err
		}
	}
	return nil
}
