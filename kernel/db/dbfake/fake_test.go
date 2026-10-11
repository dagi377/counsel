package dbfake

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

func TestSessionDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	f := New()
	if err := f.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return f.Bind(ctx, tx, db.Session{UserID: "u1", PrincipalKind: "user"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if v := tx.(*Tx).Vars()[db.VarUserID]; v != "" {
			t.Fatalf("second transaction sees %s=%q", db.VarUserID, v)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInTx(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name          string
		close         bool
		fn            func(ctx context.Context, tx pgx.Tx) error
		err           error
		commits, robs int
	}{
		{name: "commit", fn: func(context.Context, pgx.Tx) error { return nil }, commits: 1},
		{name: "rollback", fn: func(context.Context, pgx.Tx) error { return boom }, err: boom, robs: 1},
		{name: "closed", close: true, fn: func(context.Context, pgx.Tx) error { return nil }, err: db.ErrClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if tt.close {
				f.Close()
			}
			if err := f.InTx(context.Background(), tt.fn); !errors.Is(err, tt.err) {
				t.Fatalf("InTx err = %v, want %v", err, tt.err)
			}
			if f.Commits != tt.commits || f.Rollbacks != tt.robs {
				t.Fatalf("commits %d rollbacks %d", f.Commits, f.Rollbacks)
			}
			if tt.close {
				if err := f.Ping(context.Background()); !errors.Is(err, db.ErrClosed) {
					t.Fatalf("Ping err = %v", err)
				}
			}
		})
	}
}

func TestBind(t *testing.T) {
	tests := []struct {
		name string
		s    db.Session
		want map[string]string
	}{
		{name: "user", s: db.Session{UserID: "u1", PrincipalKind: "user"}, want: map[string]string{db.VarUserID: "u1", db.VarPrincipalKind: "user"}},
		{name: "agent", s: db.Session{PrincipalKind: "agent", RunID: "r1", ActingFor: "u1"},
			want: map[string]string{db.VarPrincipalKind: "agent", db.VarRunID: "r1", db.VarActingFor: "u1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			_ = f.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
				if err := f.Bind(ctx, tx, tt.s); err != nil {
					t.Fatal(err)
				}
				got := tx.(*Tx).Vars()
				if len(got) != len(tt.want) {
					t.Fatalf("vars = %v, want %v", got, tt.want)
				}
				for k, v := range tt.want {
					if got[k] != v {
						t.Fatalf("vars = %v, want %v", got, tt.want)
					}
				}
				return nil
			})
		})
	}
	if err := New().Bind(context.Background(), nil, db.Session{}); !errors.Is(err, db.ErrNoTx) {
		t.Fatalf("Bind nil tx err = %v", err)
	}
}
