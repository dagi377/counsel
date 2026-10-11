//go:build integration

package db_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	return dsn
}

// withApp returns dsn with application_name set, in URL or keyword/value form.
func withApp(dsn, name string) string {
	if !strings.Contains(dsn, "://") {
		return dsn + " application_name=" + name
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&application_name=" + name
	}
	return dsn + "?application_name=" + name
}

func TestARC_U_011_SessionDoesNotLeakAcrossRequests(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	// One pooled connection, so both requests share the same server session.
	p, err := db.Open(ctx, db.Config{PooledDSN: dsn, DirectDSN: withApp(dsn, "direct"), MaxConns: 1})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer p.Close()

	readVar := func(ctx context.Context, tx pgx.Tx, name string) (string, int32) {
		t.Helper()
		var v string
		var pid int32
		if err := tx.QueryRow(ctx, "SELECT coalesce(current_setting($1, true), ''), pg_backend_pid()", name).Scan(&v, &pid); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return v, pid
	}

	tests := []struct {
		name    string
		session db.Session
		failA   bool
	}{
		{name: "user committed", session: db.Session{UserID: "u-1", PrincipalKind: "user"}},
		{name: "agent committed", session: db.Session{UserID: "u-2", PrincipalKind: "agent", RunID: "r-1", ActingFor: "u-3"}},
		{name: "user rolled back", session: db.Session{UserID: "u-4", PrincipalKind: "user"}, failA: true},
	}
	errA := errors.New("request A fails")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var pidA int32
			// rule: ARC-U-011
			err := p.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				if err := p.Bind(ctx, tx, tc.session); err != nil {
					return err
				}
				var got string
				got, pidA = readVar(ctx, tx, db.VarUserID)
				if got != tc.session.UserID {
					t.Fatalf("request A %s = %q, want %q", db.VarUserID, got, tc.session.UserID)
				}
				if tc.failA {
					return errA
				}
				return nil
			})
			if tc.failA && !errors.Is(err, errA) || !tc.failA && err != nil {
				t.Fatalf("request A: %v", err)
			}
			err = p.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				for name := range tc.session.Vars() {
					got, pidB := readVar(ctx, tx, name)
					if pidB != pidA {
						t.Fatalf("request B on backend %d, want same connection %d", pidB, pidA)
					}
					if got != "" {
						t.Fatalf("request B %s = %q, want empty", name, got)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("request B: %v", err)
			}
		})
	}
}

func TestARC_U_011_BindRejectsForeignTx(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	p, err := db.Open(ctx, db.Config{PooledDSN: dsn, DirectDSN: withApp(dsn, "direct")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer p.Close()
	other, err := db.Open(ctx, db.Config{PooledDSN: dsn, DirectDSN: withApp(dsn, "direct")})
	if err != nil {
		t.Fatalf("Open other: %v", err)
	}
	defer other.Close()

	tests := []struct {
		name string
		via  *db.Pool
	}{
		{name: "tx from another pool", via: other},
		{name: "nil tx", via: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.via == nil {
				if err := p.Bind(ctx, nil, db.Session{UserID: "u"}); !errors.Is(err, db.ErrNoTx) {
					t.Fatalf("Bind(nil) = %v, want ErrNoTx", err)
				}
				return
			}
			err := tc.via.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return p.Bind(ctx, tx, db.Session{UserID: "u"})
			})
			if !errors.Is(err, db.ErrNoTx) {
				t.Fatalf("Bind = %v, want ErrNoTx", err)
			}
		})
	}
}

func TestARC_U_011_DirectPoolForJobQueue(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	tests := []struct {
		name    string
		cfg     db.Config
		wantErr bool
	}{
		// rule: ARC-U-011
		{name: "direct is the pooler", cfg: db.Config{PooledDSN: dsn, DirectDSN: dsn}, wantErr: true},
		{name: "direct missing", cfg: db.Config{PooledDSN: dsn}, wantErr: true},
		{name: "direct set", cfg: db.Config{PooledDSN: withApp(dsn, "pooled"), DirectDSN: withApp(dsn, "direct")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dp, err := db.OpenDirect(ctx, tc.cfg)
			if tc.wantErr {
				if err == nil {
					dp.Close()
					t.Fatal("OpenDirect: want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("OpenDirect: %v", err)
			}
			defer dp.Close()
			var app string
			if err := dp.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&app); err != nil {
				t.Fatalf("query: %v", err)
			}
			if app != "direct" {
				t.Fatalf("job queue pool connected as %q, want the direct dsn", app)
			}
		})
	}
}

func TestPoolClosed(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	p, err := db.Open(ctx, db.Config{PooledDSN: dsn, DirectDSN: withApp(dsn, "direct")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p.Close()
	tests := []struct {
		name string
		call func() error
	}{
		{name: "ping", call: func() error { return p.Ping(ctx) }},
		{name: "in tx", call: func() error { return p.InTx(ctx, func(context.Context, pgx.Tx) error { return nil }) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, db.ErrClosed) {
				t.Fatalf("err = %v, want ErrClosed", err)
			}
		})
	}
}
