//go:build integration

package payment_test

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

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/matter/mattertest"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// poolTx runs fn in one transaction on a pool.
type poolTx struct{ pool *pgxpool.Pool }

func (p poolTx) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
}

// pgEnv is a fresh migrated database with one delivered bill: client, 80000 fees and 10400 tax.
type pgEnv struct {
	pool              *pgxpool.Pool
	user, client, bil string
	other             string
}

func newPGEnv(t *testing.T) pgEnv {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "payment_" + hex.EncodeToString(suffix)
	unlock := migrationstest.LockClusterRoles(ctx, t, admin)
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
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	err = migrations.Apply(ctx, conn, "")
	unlock()
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	env := pgEnv{pool: pool}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var role, area, matter string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id) VALUES ('u@example.com', 'U', $1::uuid) RETURNING id::text`, role).Scan(&env.user); err != nil {
			return err
		}
		for _, id := range []*string{&env.client, &env.other} {
			if err := tx.QueryRow(ctx, `INSERT INTO party.contact(kind, legal_name, display_name) VALUES ('person', 'C', 'C') RETURNING id::text`).Scan(id); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO matter.practice_area(name) VALUES ('Litigation') RETURNING id::text`).Scan(&area); err != nil {
			return err
		}
		if matter, err = mattertest.Seed(ctx, tx, "2026-0001", "M", env.client, area, env.user); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO billing.bill (number, matter_id, payer_id, status, issue_date, fees_cents, tax_cents,
total_cents, approval_id, approved_by, place_of_supply_province, delivered_at, created_by)
VALUES ('B-1', $1::uuid, $2::uuid, 'delivered', current_date, 80000, 10400, 90400, core.uuidv7(), $3::uuid, 'ON', now(), $3::uuid)
RETURNING id::text`, matter, env.client, env.user).Scan(&env.bil)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return env
}

func (e pgEnv) capture(amount int64) payment.Capture {
	return payment.Capture{ClientID: e.client, BillID: e.bil, Kind: payment.KindBillPayment, AmountCents: amount,
		ReceivedOn: time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), Method: "eft", RecordedBy: e.user}
}

// TestPGServiceContract runs the paymentfake contract cases against Postgres.
func TestPGServiceContract(t *testing.T) {
	tests := []struct {
		name    string
		c       func(pgEnv) payment.Capture
		want    []payment.Allocation
		wantErr error
	}{
		{name: "partial tax share", c: func(e pgEnv) payment.Capture { return e.capture(50000) },
			want: []payment.Allocation{{FeeCents: 44248, TaxCents: 5752}}},
		{name: "overpayment unapplied", c: func(e pgEnv) payment.Capture { return e.capture(100000) },
			want: []payment.Allocation{{FeeCents: 80000, TaxCents: 10400}, {FeeCents: 9600}}},
		{name: "retainer", c: func(e pgEnv) payment.Capture { c := e.capture(100); c.Kind = payment.KindRetainer; return c },
			wantErr: payment.ErrRetainerToOperating},
		{name: "other client", c: func(e pgEnv) payment.Capture { c := e.capture(100); c.ClientID = e.other; return c },
			wantErr: payment.ErrOtherClient},
		{name: "zero", c: func(e pgEnv) payment.Capture { return e.capture(0) }, wantErr: payment.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			env := newPGEnv(t)
			svc := payment.PGService{DB: poolTx{env.pool}, Payers: namedPayers{}}
			c := tt.c(env)
			p, got, err := svc.Capture(ctx, c)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Capture err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("allocations = %+v, want %+v", got, tt.want)
			}
			for i, a := range got {
				if a.FeeCents != tt.want[i].FeeCents || a.TaxCents != tt.want[i].TaxCents || a.PaymentID != p.ID {
					t.Errorf("allocation %d = %+v, want %+v", i, a, tt.want[i])
				}
			}
			reversed, err := svc.Reverse(ctx, p.ID, payment.ReasonNSF, payment.Actor{UserID: env.user})
			if err != nil || reversed.Status != payment.StatusReversed || reversed.Reason != payment.ReasonNSF || reversed.ReversedBy != env.user {
				t.Fatalf("Reverse = %+v, %v", reversed, err)
			}
			if _, err := svc.Reverse(ctx, p.ID, payment.ReasonNSF, payment.Actor{UserID: env.user}); !errors.Is(err, payment.ErrReversed) {
				t.Fatalf("second Reverse err = %v, want ErrReversed", err)
			}
			ps, err := svc.List(ctx, payment.ListFilter{Limit: 10, ClientID: env.client})
			if err != nil || len(ps) == 0 {
				t.Fatalf("List = %+v, %v", ps, err)
			}
		})
	}
}

func TestPGServiceCaptureDedup(t *testing.T) {
	tests := []struct {
		name string
		edit func(*payment.Capture)
	}{
		{name: "provider event", edit: func(c *payment.Capture) { c.Provider, c.ProviderEventID = "card", "evt_1" }},
		{name: "idempotency key", edit: func(c *payment.Capture) { c.IdempotencyKey = "k1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			env := newPGEnv(t)
			svc := payment.PGService{DB: poolTx{env.pool}, Payers: namedPayers{}}
			c := env.capture(1000)
			tt.edit(&c)
			a, _, err := svc.Capture(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			b, _, err := svc.Capture(ctx, c)
			if err != nil || a.ID != b.ID {
				t.Fatalf("two payments %s and %s (%v), want one", a.ID, b.ID, err)
			}
		})
	}
}

func TestPGProposalsContract(t *testing.T) {
	ctx := context.Background()
	env := newPGEnv(t)
	svc := payment.PGService{DB: poolTx{env.pool}, Payers: namedPayers{}}
	p, _, err := svc.Capture(ctx, env.capture(50000))
	if err != nil {
		t.Fatal(err)
	}
	var got []payment.Allocation
	err = poolTx{env.pool}.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, err = payment.PGProposals{}.Propose(ctx, tx, p.ID, 1,
			[]payment.ProposedAllocation{{TargetType: "bill", TargetID: env.bil, AmountCents: 50000}}, env.user)
		return err
	})
	if err != nil || len(got) != 1 || got[0].FeeCents != 44248 || got[0].TaxCents != 5752 {
		t.Fatalf("Propose = %+v, %v", got, err)
	}
}

// namedPayers names every payer "C", as the party module would.
type namedPayers struct{}

func (namedPayers) PayerName(context.Context, string) (string, error) { return "C", nil }
