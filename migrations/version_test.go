package migrations

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeRow answers one Applied query.
type fakeRow struct {
	ok  bool
	err error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*bool)) = r.ok
	return nil
}

// fakeDB returns its answers in order and repeats the last one.
type fakeDB struct {
	answers []fakeRow
	calls   int
	names   []any
}

func (f *fakeDB) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	f.names = append(f.names, args...)
	i := min(f.calls, len(f.answers)-1)
	f.calls++
	return f.answers[i]
}

func TestLatestIsNewestMigration(t *testing.T) {
	names := Names()
	if got := Latest(); got != names[len(names)-1] || got == "0001_init.sql" {
		t.Fatalf("Latest() = %q, want the newest of %v", got, names)
	}
}

func TestStatementsSplitsNoTransactionFile(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "comments and blank lines dropped",
			sql:  "-- module: kernel/x\n-- migrate: no-transaction\n\nSET lock_timeout = '2s';\nCREATE INDEX CONCURRENTLY IF NOT EXISTS i ON t (a) -- why\n  WHERE a IS NOT NULL;\nRESET lock_timeout;\n",
			want: []string{"SET lock_timeout = '2s'", "CREATE INDEX CONCURRENTLY IF NOT EXISTS i ON t (a) \n  WHERE a IS NOT NULL", "RESET lock_timeout"},
		},
		{name: "empty", sql: "-- only a comment\n", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statements(tt.sql); !slices.Equal(got, tt.want) {
				t.Fatalf("statements() = %q, want %q", got, tt.want)
			}
		})
	}
}

var simpleStmt = regexp.MustCompile(`^(?i)(SET|RESET|(CREATE (UNIQUE )?|DROP )INDEX CONCURRENTLY)\b`)

// Every no-transaction migration holds only statements statements() can split exactly.
func TestNoTransactionFilesSplit(t *testing.T) {
	found := 0
	for _, name := range Names() {
		sql, err := Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if !noTransaction.Match(sql) {
			continue
		}
		found++
		for _, s := range statements(string(sql)) {
			if !simpleStmt.MatchString(s) {
				t.Errorf("%s: statement %q is not SET, RESET or a concurrent index", name, s)
			}
		}
	}
	if found == 0 {
		t.Fatal("no no-transaction migration found")
	}
}

func TestApplied(t *testing.T) {
	boom := errors.New("connection refused")
	tests := []struct {
		name    string
		row     fakeRow
		want    bool
		wantErr bool
	}{
		{name: "applied", row: fakeRow{ok: true}, want: true},
		{name: "not yet", row: fakeRow{ok: false}},
		{name: "no version table", row: fakeRow{err: &pgconn.PgError{Code: "42P01"}}},
		{name: "no version schema", row: fakeRow{err: &pgconn.PgError{Code: "3F000"}}},
		{name: "other error", row: fakeRow{err: boom}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeDB{answers: []fakeRow{tt.row}}
			got, err := Applied(context.Background(), db, "0300_gate_payload_recommend.sql")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Applied() = %v, %v; want %v, err %v", got, err, tt.want, tt.wantErr)
			}
			if len(db.names) != 1 || db.names[0] != "0300_gate_payload_recommend.sql" {
				t.Fatalf("queried %v", db.names)
			}
		})
	}
}

func TestWait(t *testing.T) {
	boom := errors.New("connection reset")
	missing := fakeRow{err: &pgconn.PgError{Code: "42P01"}}
	tests := []struct {
		name      string
		version   string
		answers   []fakeRow
		timeout   time.Duration
		wantErr   error
		wantCalls int
	}{
		{name: "already applied", version: Latest(), answers: []fakeRow{{ok: true}}, timeout: time.Second, wantCalls: 1},
		{name: "applied after polls", version: Latest(), answers: []fakeRow{missing, {ok: false}, {ok: true}}, timeout: time.Second, wantCalls: 3},
		{name: "query error returns to reconnect", version: Latest(), answers: []fakeRow{missing, {err: boom}}, timeout: time.Second, wantErr: boom, wantCalls: 2},
		{name: "never applied", version: Latest(), answers: []fakeRow{{ok: false}}, timeout: 30 * time.Millisecond, wantErr: context.DeadlineExceeded},
		{name: "unknown version", version: "9999_nothing.sql", answers: []fakeRow{{ok: true}}, timeout: time.Second, wantErr: ErrUnknownVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()
			db := &fakeDB{answers: tt.answers}
			err := Wait(ctx, db, tt.version, time.Millisecond)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Wait() = %v, want %v", err, tt.wantErr)
			}
			if tt.wantCalls > 0 && db.calls != tt.wantCalls {
				t.Fatalf("polled %d times, want %d", db.calls, tt.wantCalls)
			}
		})
	}
}
