//go:build integration

package recon_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// signOffEnv is a migrated database with a trust account opened 2026-03-01, a balanced draft
// March reconciliation and an unbalanced draft April one, both prepared by prep, and an
// approved trust.reconciliation_signoff gate.
type signOffEnv struct {
	pool                  *db.Pool
	prep, lawyer, gate    string
	account, march, april string
}

func newSignOffEnv(t *testing.T) signOffEnv {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "recon_signoff_" + hex.EncodeToString(b)
	defer migrationstest.LockClusterRoles(ctx, t, admin)()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	err = migrations.Apply(ctx, conn, "")
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(pool.Close)

	env := signOffEnv{pool: pool}
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for _, u := range []struct {
			name string
			id   *string
		}{{"prep", &env.prep}, {"lawyer1", &env.lawyer}} {
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id) VALUES ($1 || '@example.com', $1, $2::uuid) RETURNING id::text`,
				u.name, role).Scan(u.id); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO trust.trust_account(type, institution, account_no_ciphertext, account_no_last4, title, opened_on)
VALUES ('mixed', 'Bank', '\x00'::bytea, '1234', 'Mixed trust', '2026-03-01') RETURNING id::text`).Scan(&env.account); err != nil {
			return err
		}
		insert := func(month string, adjusted int64) (string, error) {
			var id string
			err := tx.QueryRow(ctx, `INSERT INTO trust.reconciliation(account_id, month, bank_balance_cents, adjusted_bank_cents,
  journal_balance_cents, client_listing_total_cents, client_listing_snapshot, prepared_by, status)
VALUES ($1::uuid, $2::date, $3, $3, 0, 0, '[]'::jsonb, $4::uuid, 'draft') RETURNING id::text`, env.account, month, adjusted, env.prep).Scan(&id)
			return id, err
		}
		var err error
		if env.march, err = insert("2026-03-01", 0); err != nil {
			return err
		}
		if env.april, err = insert("2026-04-01", 100); err != nil {
			return err
		}
		jcs := []byte(`{"reconciliation_id":"` + env.march + `"}`)
		sum := sha256.Sum256(jcs)
		return tx.QueryRow(ctx, `INSERT INTO gate.approval_request(gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, status, expires_at,
  payload_jcs, payload_sha256)
VALUES ('trust.reconciliation_signoff', 'G12', 'trust.reconciliation', $1::uuid, $3, $2::uuid, 'approved', now() + interval '1 day', $4, $3) RETURNING id::text`,
			env.march, env.prep, sum[:], jcs).Scan(&env.gate)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return env
}

// rule: BIL-I-025
func TestBIL_I_025_PGSignOffIntegration(t *testing.T) {
	env := newSignOffEnv(t)
	ctx := bylaw9Ctx(t, true)
	s := recon.NewPGSignOff(env.pool)
	march := recon.Period{Year: 2026, Month: time.March}
	pkg := sha256.Sum256([]byte("package"))
	at := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
	rec := func(id, by string) recon.SignOffRecord {
		return recon.SignOffRecord{ReconciliationID: id, By: recon.Approver{UserID: by}, At: at, PackageSHA256: pkg[:], ApprovalID: env.gate}
	}
	sign := func(r recon.SignOffRecord) error {
		return env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return s.Sign(ctx, tx, r) })
	}
	steps := []struct {
		name       string
		rec        recon.SignOffRecord
		want       error
		wantLocked bool
	}{
		{"preparer cannot sign", rec(env.march, env.prep), recon.ErrSelfApproval, false},
		{"unbalanced recon is refused", rec(env.april, env.lawyer), recon.ErrUnbalanced, false},
		{"unknown recon", rec("00000000-0000-7000-8000-00000000dead", env.lawyer), recon.ErrNotFound, false},
		{"lawyer signs March", rec(env.march, env.lawyer), nil, true},
		{"second sign-off is refused", rec(env.march, env.lawyer), recon.ErrInvalidState, true},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			if err := sign(st.rec); !errors.Is(err, st.want) {
				t.Fatalf("Sign = %v, want %v", err, st.want)
			}
			locked, err := s.Locked(ctx, env.account, march)
			if err != nil || locked != st.wantLocked {
				t.Fatalf("Locked(March) = %v, %v; want %v", locked, err, st.wantLocked)
			}
		})
	}
	var by string
	var signedAt time.Time
	var stored []byte
	if err := env.pool.QueryRow(ctx, `SELECT signed_by::text, signed_at, package_sha256 FROM trust.reconciliation WHERE id = $1::uuid AND status = 'signed'`,
		env.march).Scan(&by, &signedAt, &stored); err != nil {
		t.Fatalf("read sign-off: %v", err)
	}
	if by != env.lawyer || !signedAt.Equal(at) || string(stored) != string(pkg[:]) {
		t.Fatalf("stored sign-off = %s %v %x", by, signedAt, stored)
	}
	posting := []struct {
		date clock.Date
		want error
	}{
		{clock.Date{Year: 2026, Month: time.March, Day: 31}, recon.ErrPeriodLocked},
		{clock.Date{Year: 2026, Month: time.April, Day: 2}, nil},
	}
	for _, p := range posting {
		if err := recon.CheckPostingDate(ctx, s, env.account, p.date); !errors.Is(err, p.want) {
			t.Fatalf("posting %s: %v, want %v", p.date, err, p.want)
		}
	}
}
