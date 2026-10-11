//go:build integration

package deadline_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/migrations"
)

// poolRunner runs fn in one pool transaction, like kernel/db.DB.InTx.
type poolRunner struct{ pool *pgxpool.Pool }

func (r poolRunner) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
}

// deadlineDB creates a throwaway database with every migration applied and one user. The
// app's own link CHECK on cal.deadline (one app link column set) is dropped in this database
// only, so the kernel store can be exercised with a domain-free subject kind. It returns the
// pool and the user id.
func deadlineDB(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	name := "deadline_store_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if err := migrations.Apply(ctx, conn.Conn(), ""); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	var check string
	if err := conn.QueryRow(ctx, `SELECT conname FROM pg_constraint
 WHERE conrelid = 'cal.deadline'::regclass AND contype = 'c'
   AND pg_get_constraintdef(oid) LIKE '%lead_id IS NULL%'`).Scan(&check); err != nil {
		t.Fatalf("find app link check: %v", err)
	}
	if _, err := conn.Exec(ctx, "ALTER TABLE cal.deadline DROP CONSTRAINT "+pgx.Identifier{check}.Sanitize()); err != nil {
		t.Fatalf("drop app link check: %v", err)
	}
	var user string
	if err := conn.QueryRow(ctx, `WITH r AS (INSERT INTO core.role(key, name, status) VALUES ('scheduler', 'Scheduler', 'active') RETURNING id)
INSERT INTO core.app_user(email, display_name, role_id) SELECT 'd@example.com', 'D', id FROM r RETURNING id::text`).Scan(&user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return pool, user
}

// The store on Postgres: save, confirm once by a person, refuse an agent and a second
// confirm, and repack only the unconfirmed deadline.
func TestPGBookOnPostgres(t *testing.T) {
	pool, user := deadlineDB(t)
	ctx := context.Background()
	b := deadline.NewPGBook(poolRunner{pool})
	v1 := deadline.Pack{ID: "helpdesk_sla", Version: "1", Rules: []deadline.Rule{
		{ID: "auto_close", Mode: deadline.Calendar, Days: 14},
		{ID: "first_response", Mode: deadline.Business, Days: 1, Roll: deadline.RollForward},
	}}
	if err := b.LoadPack(ctx, v1); err != nil {
		t.Fatal(err)
	}
	subject := deadline.Subject{Kind: "ticket", ID: "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}
	trigger := deadline.Date{Year: 2026, Month: time.March, Day: 2}
	by := deadline.Principal{Kind: registry.PrincipalUser, UserID: user}
	bot := deadline.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: user}

	var confirmed, open deadline.DeadlineID
	err := poolRunner{pool}.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, rule := range []string{"auto_close", "first_response"} {
			ps, err := b.Compute(ctx, v1.ID, rule, trigger, subject)
			if err != nil {
				return err
			}
			id, err := b.Save(ctx, tx, ps[0])
			if err != nil {
				return err
			}
			if rule == "auto_close" {
				confirmed = id
			} else {
				open = id
			}
		}
		if err := b.Confirm(ctx, tx, confirmed, bot); !errors.Is(err, deadline.ErrAgentCannotConfirm) {
			t.Errorf("agent Confirm = %v, want ErrAgentCannotConfirm", err)
		}
		if err := b.Confirm(ctx, tx, confirmed, by); err != nil {
			return err
		}
		if err := b.Confirm(ctx, tx, confirmed, by); !errors.Is(err, deadline.ErrAlreadyConfirmed) {
			t.Errorf("second Confirm = %v, want ErrAlreadyConfirmed", err)
		}
		v2 := v1
		v2.Version = "2"
		v2.Rules = []deadline.Rule{
			{ID: "auto_close", Mode: deadline.Calendar, Days: 21},
			{ID: "first_response", Mode: deadline.Business, Days: 2, Roll: deadline.RollForward},
		}
		changed, err := b.Repack(ctx, tx, v2)
		if err != nil {
			return err
		}
		if len(changed) != 1 || changed[0] != open {
			t.Errorf("Repack changed %v, want only %s", changed, open)
		}
		log, err := b.Confirmations(ctx, tx, confirmed)
		if err != nil {
			return err
		}
		if len(log) != 1 || log[0].UserID != user {
			t.Errorf("confirmations = %+v, want one by %s", log, user)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		id          deadline.DeadlineID
		wantState   deadline.State
		wantVersion string
		wantDue     deadline.Date
	}{
		{"confirmed kept", confirmed, deadline.StateConfirmed, "1", deadline.Date{Year: 2026, Month: time.March, Day: 16}},
		{"unconfirmed recomputed", open, deadline.StateProposed, "2", deadline.Date{Year: 2026, Month: time.March, Day: 4}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := b.Get(ctx, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if d.State != tc.wantState || d.Proposed.PackVersion != tc.wantVersion || d.Proposed.Due != tc.wantDue || d.Proposed.Subject != subject {
				t.Fatalf("Get = %+v, want %s v%s due %s", d, tc.wantState, tc.wantVersion, tc.wantDue)
			}
		})
	}
}
