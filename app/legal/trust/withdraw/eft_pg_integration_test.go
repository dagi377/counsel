//go:build integration

package withdraw

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

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/matter/mattertest"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/migrations"
	"github.com/dagi377/counsel/migrations/migrationstest"
)

// eftEnv is a database with every migration applied, two EFT withdrawals w1 and w2 of 4000
// cents, and a final Form 9A document bound to each (doc[i] to wid[i]), signed by lawyer.
type eftEnv struct {
	pool   *db.Pool
	lawyer string
	wid    [2]string
	doc    [2]string
	unsig  string // a final document bound to no withdrawal and never signed
	signed time.Time
}

func newEFTEnv(t *testing.T) eftEnv {
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
	name := "withdraw_eft_" + hex.EncodeToString(b)
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

	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, loc)
	env := eftEnv{pool: pool, signed: now.Add(-time.Hour)}
	gl := ledgerfake.New(clock.DateOf(now, loc), books.AcctTrustBank, books.AcctClientTrustLedger)
	accts := trust.NewAccountStore(pool, itDir{}, itSeal{})
	store := trust.NewLedgerStore(pool, accts, gl, clockfake.New(now, loc))
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var role, prep, client, area, matter string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for _, u := range []struct {
			name string
			id   *string
		}{{"prep", &prep}, {"lawyer1", &env.lawyer}} {
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id) VALUES ($1 || '@example.com', $1, $2::uuid) RETURNING id::text`,
				u.name, role).Scan(u.id); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO party.contact(kind, legal_name, display_name) VALUES ('person', 'C', 'C') RETURNING id::text`).Scan(&client); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO matter.practice_area(name) VALUES ('Litigation') RETURNING id::text`).Scan(&area); err != nil {
			return err
		}
		var err error
		if matter, err = mattertest.Seed(ctx, tx, "2026-0001", "M", client, area, env.lawyer); err != nil {
			return err
		}
		acct, err := accts.OpenAccountNumber(ctx, tx, trust.Account{Type: trust.AccountMixed, Institution: "Bank",
			AccountLast4: "1234", Title: "Mixed trust", OpenedOn: now}, "000111234")
		if err != nil {
			return err
		}
		ledger, err := store.OpenLedger(ctx, tx, trust.Ledger{AccountID: acct, MatterID: matter, ClientID: client})
		if err != nil {
			return err
		}
		newDoc := func(title string) (string, error) {
			var id string
			err := tx.QueryRow(ctx, `INSERT INTO docs.document(matter_id, title, doc_type, source, status, privilege)
VALUES ($1::uuid, $2, 'form_9a', 'upload', 'final', 'confidential') RETURNING id::text`, matter, title).Scan(&id)
			return id, err
		}
		for i := range env.wid {
			if env.doc[i], err = newDoc("Form 9A"); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `INSERT INTO trust.withdrawal_request(ledger_id, purpose_category, payee, amount_cents, method,
  receiving_account, form_9a_doc_id, initiated_by)
VALUES ($1::uuid, 'disbursement', 'Vendor Inc', 4000, 'eft', '000-111', $2::uuid, $3::uuid) RETURNING id::text`,
				string(ledger), env.doc[i], prep).Scan(&env.wid[i]); err != nil {
				return err
			}
			if err := (PGEFTRecords{}).RecordSignature(ctx, tx, env.doc[i], env.lawyer, env.signed); err != nil {
				return err
			}
		}
		env.unsig, err = newDoc("Unsigned Form 9A")
		return err
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return env
}

// TestPGEFTRecordsIntegration reads Form 9A records on the migrated schema and runs CheckEFT
// over them for withdrawal w1.
func TestPGEFTRecordsIntegration(t *testing.T) {
	env := newEFTEnv(t)
	at := env.signed.Add(time.Hour)
	held := func(rec string) Held {
		return Held{Withdrawal: Withdrawal{ID: env.wid[0], AuthorizedBy: env.lawyer,
			Request: Request{AmountCents: 4000, Method: MethodEFT, EFTAuthorizationID: rec}}}
	}
	tests := []struct {
		name      string
		rec       string
		wantFetch string // withdrawal the reader returns for rec; "" when it returns none
		want      error
	}{
		{"record found", env.doc[0], env.wid[0], nil},
		{"unsigned record: refused", env.unsig, "", ErrNoEFTAuthorization},
		{"no such record: refused", "00000000-0000-7000-8000-00000000dead", "", ErrNoEFTAuthorization},
		{"record for another withdrawal: refused", env.doc[1], env.wid[1], ErrNoEFTAuthorization},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
				a, err := PGEFTRecords{}.EFTAuthorization(ctx, tx, tt.rec)
				if tt.wantFetch == "" && !errors.Is(err, ErrNotFound) {
					t.Errorf("EFTAuthorization = %+v, %v; want ErrNotFound", a, err)
				}
				if tt.wantFetch != "" && (err != nil || a.WithdrawalID != tt.wantFetch || a.SignedBy != env.lawyer || !a.SignedAt.Equal(env.signed)) {
					t.Errorf("EFTAuthorization = %+v, %v; want record of %s signed by %s", a, err, tt.wantFetch, env.lawyer)
				}
				if err := CheckEFT(ctx, tx, PGEFTRecords{}, held(tt.rec), at); !errors.Is(err, tt.want) {
					t.Errorf("CheckEFT = %v, want %v", err, tt.want)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
