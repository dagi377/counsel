package migrations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The schema version of a database is the set of migration files applied to it, recorded by
// Apply in versionTable (one row per file name). The table is runner bookkeeping, not part of
// the domain schema, so Apply creates it itself before the first file runs. It holds only file
// names and times, so every role may read it: the app pods' wait step uses the app role.
const versionTable = "schema_meta.applied"

const ensureVersionTable = `CREATE SCHEMA IF NOT EXISTS schema_meta;
CREATE TABLE IF NOT EXISTS schema_meta.applied (
  name       text PRIMARY KEY CHECK (name ~ '^[0-9]{4}_[a-z0-9_]+\.sql$'),
  applied_at timestamptz NOT NULL DEFAULT now()
);
GRANT USAGE ON SCHEMA schema_meta TO PUBLIC;
GRANT SELECT ON schema_meta.applied TO PUBLIC`

// noTransaction marks a file whose statements must run one by one, outside a transaction
// (CREATE INDEX CONCURRENTLY). tools/migration_lint.py allows only SET, RESET and concurrent
// index statements in such a file.
var noTransaction = regexp.MustCompile(`(?mi)^--[ \t]*migrate:[ \t]*no-transaction[ \t]*$`)

// Sentinel errors.
var (
	ErrUnknownVersion = errors.New("migrations: unknown schema version")
)

// Querier is the read side of a Postgres connection or pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Latest returns the name of the newest embedded migration: the schema version this build
// expects.
func Latest() string {
	names := Names()
	if len(names) == 0 {
		return ""
	}
	return names[len(names)-1]
}

// statements splits a no-transaction file into its statements. Such a file holds only simple
// statements (SET, RESET, CREATE/DROP INDEX CONCURRENTLY) with no quoted semicolons, so a split
// on ";" after removing line comments is exact.
func statements(sql string) []string {
	var b strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	var out []string
	for _, s := range strings.Split(b.String(), ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// applied returns the file names recorded in versionTable.
func applied(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, "SELECT name FROM "+versionTable)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

// applyOne runs one file and records it. A transactional file and its record commit together;
// a no-transaction file runs statement by statement and is recorded after its last statement.
func applyOne(ctx context.Context, conn *pgx.Conn, name, sql string) error {
	// name matches the numbered pattern (Names), so it is safe to inline as a literal.
	record := fmt.Sprintf("INSERT INTO %s (name) VALUES ('%s')", versionTable, name)
	if !noTransaction.MatchString(sql) {
		if _, err := conn.PgConn().Exec(ctx, sql+"\n;\n"+record).ReadAll(); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		return nil
	}
	for _, stmt := range statements(sql) {
		if _, err := conn.Exec(ctx, stmt, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	if _, err := conn.Exec(ctx, record, pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

// Applied reports whether migration name is recorded on the database. A database that has
// never been migrated (no version table yet) has applied nothing.
func Applied(ctx context.Context, q Querier, name string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM "+versionTable+" WHERE name = $1)", name).Scan(&ok)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "42P01" || pg.Code == "3F000") { // undefined table / schema
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read schema version: %w", err)
	}
	return ok, nil
}

// Wait polls the database every interval until migration name is applied. It returns
// ctx.Err() when ctx ends first, and a query error at once so the caller can reconnect.
// name must be an embedded migration, so a typo fails at once instead of waiting forever.
func Wait(ctx context.Context, q Querier, name string, every time.Duration) error {
	if !slices.Contains(Names(), name) {
		return fmt.Errorf("wait for %q: %w", name, ErrUnknownVersion)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		ok, err := Applied(ctx, q, name)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for schema %s: %w", name, ctx.Err())
		case <-t.C:
		}
	}
}
