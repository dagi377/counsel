//go:build integration

package withdraw

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/matter/mattertest"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// itEnv is a migrated database with a funded client ledger and a withdrawal awaiting its gate.
type itEnv struct {
	pool   *db.Pool
	users  map[string]string // name -> user id: prep, lawyer1, bookkeeper
	ledger trust.LedgerID
	acct   trust.AccountID
	store  *trust.LedgerStore
	now    time.Time
}

func newItEnv(t *testing.T, balance int64) itEnv {
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
	name := "trust_withdraw_" + hex.EncodeToString(b)
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
	ddl, err := os.ReadFile(filepath.FromSlash("../../../../migrations/0001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.PgConn().Exec(ctx, string(ddl)).ReadAll()
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	env := itEnv{pool: pool, users: map[string]string{}, now: time.Date(2026, 3, 16, 12, 0, 0, 0, loc)}
	clk := clockfake.New(env.now, loc)
	gl := ledgerfake.New(clock.DateOf(env.now, loc), books.AcctTrustBank, books.AcctClientTrustLedger)
	accts := trust.NewAccountStore(pool, itDir{}, itSeal{})
	env.store = trust.NewLedgerStore(pool, accts, gl, clk)
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for _, u := range []string{"prep", "lawyer1", "bookkeeper"} {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id) VALUES ($1 || '@example.com', $1, $2::uuid) RETURNING id::text`, u, role).Scan(&id); err != nil {
				return err
			}
			env.users[u] = id
		}
		var client, area, matter string
		if err := tx.QueryRow(ctx, `INSERT INTO party.contact(kind, legal_name, display_name) VALUES ('person', 'C', 'C') RETURNING id::text`).Scan(&client); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO matter.practice_area(name) VALUES ('Litigation') RETURNING id::text`).Scan(&area); err != nil {
			return err
		}
		var err error
		if matter, err = mattertest.Seed(ctx, tx, "2026-0001", "M", client, area, env.users["lawyer1"]); err != nil {
			return err
		}
		env.acct, err = accts.OpenAccountNumber(ctx, tx, trust.Account{Type: trust.AccountMixed, Institution: "Bank",
			AccountLast4: "1234", Title: "Mixed trust", OpenedOn: env.now}, "000111234")
		if err != nil {
			return err
		}
		if err := accts.AddApprovedSignatory(ctx, tx, env.acct, env.users["lawyer1"], "00000000-0000-7000-8000-000000000001", env.now); err != nil {
			return err
		}
		if env.ledger, err = env.store.OpenLedger(ctx, tx, trust.Ledger{AccountID: env.acct, MatterID: matter, ClientID: client}); err != nil {
			return err
		}
		_, err = env.store.Receive(ctx, tx, trust.Receipt{LedgerID: env.ledger, TxnDate: env.now, Method: trust.MethodEFT,
			Counterparty: "C", AmountCents: balance, Purpose: "retainer", ClearedOn: env.now, PostedBy: env.users["prep"]})
		return err
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return env
}

// request inserts a proposed withdrawal and an approved trust.withdrawal gate bound to it.
// terms returns that gate's stored payload.
func (env itEnv) request(t *testing.T, amount int64) (string, string) {
	t.Helper()
	var wid, gid string
	err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO trust.withdrawal_request(ledger_id, purpose_category, payee, amount_cents, method, initiated_by)
VALUES ($1::uuid, 'disbursement', 'Vendor Inc', $2, 'cheque', $3::uuid) RETURNING id::text`, string(env.ledger), amount, env.users["prep"]).Scan(&wid); err != nil {
			return err
		}
		// The gate stores the JCS bytes of the payload it approved: exactly this withdrawal's terms.
		var matter string
		if err := tx.QueryRow(ctx, `SELECT matter_id::text FROM trust.client_ledger WHERE id = $1::uuid`, string(env.ledger)).Scan(&matter); err != nil {
			return err
		}
		raw, err := json.Marshal(EffectPayload{WithdrawalID: wid, MatterID: matter, AmountCents: &amount, Payee: "Vendor Inc", Method: MethodCheque})
		if err != nil {
			return err
		}
		jcs, err := gate.JCS(raw)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(jcs)
		return tx.QueryRow(ctx, `INSERT INTO gate.approval_request(gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, status, expires_at,
  payload_jcs, payload_sha256)
VALUES ('trust.withdrawal', 'G11', 'trust.withdrawal_request', $1::uuid, $3, $2::uuid, 'approved', now() + interval '1 day', $4, $3) RETURNING id::text`,
			wid, env.users["prep"], sum[:], jcs).Scan(&gid)
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return wid, gid
}

func (env itEnv) effect() *Effect {
	return &Effect{Ledgers: env.store, Store: PGStore{}, Signers: itSigners{}, Firm: itFirm{}, Audit: itAudit{}, Now: func() time.Time { return env.now },
		Approvals: PGApprovals{}}
}

// terms is the stored payload of the approved gate bound to withdrawal wid.
func (env itEnv) terms(t *testing.T, wid string) json.RawMessage {
	t.Helper()
	var raw []byte
	if err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload_jcs FROM gate.approval_request WHERE payload_id = $1::uuid`, wid).Scan(&raw)
	}); err != nil {
		t.Fatalf("terms: %v", err)
	}
	return raw
}

func (env itEnv) posted(t *testing.T) int64 {
	t.Helper()
	b, err := env.store.Balance(context.Background(), env.ledger)
	if err != nil {
		t.Fatal(err)
	}
	return b.PostedCents
}

// rule: ARC-I-027
func TestARC_I_027_EffectIntegration(t *testing.T) {
	tests := []struct {
		name     string
		executor string
		want     error
		after    int64
	}{
		{"bookkeeper records execution", "bookkeeper", nil, 6000},
		{"authorizer records execution in a 2-person firm", "lawyer1", ErrNotAuthorized, 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newItEnv(t, 10000)
			wid, gid := env.request(t, 4000)
			e := env.effect()
			ctx := context.Background()
			p := env.terms(t, wid)
			if err := env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return e.Apply(ctx, tx, registry.Approved{GateID: gid, Kind: GateKind, Payload: p,
					Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: env.users["lawyer1"]}}})
			}); err != nil {
				t.Fatalf("apply: %v", err)
			}
			err := env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err := e.Execute(ctx, tx, wid, registry.Principal{Kind: registry.PrincipalUser, UserID: env.users[tt.executor]}, env.now)
				return err
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("execute = %v, want %v", err, tt.want)
			}
			if got := env.posted(t); got != tt.after {
				t.Fatalf("ledger = %d, want %d", got, tt.after)
			}
		})
	}
}

// rule: ARC-I-024
func TestARC_I_024_EffectIntegration(t *testing.T) {
	env := newItEnv(t, 10000)
	wid, gid := env.request(t, 12000)
	p := env.terms(t, wid)
	err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return env.effect().Apply(ctx, tx, registry.Approved{GateID: gid, Kind: GateKind, Payload: p,
			Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: env.users["lawyer1"]}}})
	})
	if !errors.Is(err, gate.ErrEffectFailed) || !errors.Is(err, ErrTrustOverdraw) {
		t.Fatalf("apply = %v, want effect failed with trust overdraw", err)
	}
	if got := env.posted(t); got != 10000 {
		t.Fatalf("ledger = %d, want 10000 (no posting)", got)
	}
	var status string
	if err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM trust.withdrawal_request WHERE id = $1::uuid`, wid).Scan(&status)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "proposed" {
		t.Fatalf("status = %s, want proposed", status)
	}
}

// rule: ARC-I-025
// Twenty authorisations of 100000 race on a 1000000 ledger. Apply locks the ledger row in the
// gate's tx and records its hold there, so exactly ten pass and the holds never exceed the
// ledger.
func TestARC_I_025_ConcurrentAuthorizationsIntegration(t *testing.T) {
	const n, each, balance = 20, 100000, 1000000
	env := newItEnv(t, balance)
	type pending struct{ wid, gid string }
	reqs := make([]pending, n)
	for i := range reqs {
		reqs[i].wid, reqs[i].gid = env.request(t, each)
	}
	e := env.effect()
	terms := make([]json.RawMessage, n)
	for i, r := range reqs {
		terms[i] = env.terms(t, r.wid)
	}
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i, r := range reqs {
		wg.Go(func() {
			p := terms[i]
			<-start
			errs[i] = env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
				return e.Apply(ctx, tx, registry.Approved{GateID: r.gid, Kind: GateKind, Payload: p,
					Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: env.users["lawyer1"]}}})
			})
		})
	}
	close(start)
	wg.Wait()
	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, gate.ErrEffectFailed) && errors.Is(err, ErrTrustOverdraw):
			refused++
		default:
			t.Fatalf("apply: %v", err)
		}
	}
	if ok != balance/each || refused != n-balance/each {
		t.Fatalf("ok = %d, refused = %d; want %d, %d", ok, refused, balance/each, n-balance/each)
	}
	var authorized int
	var held int64
	if err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), coalesce(sum(amount_cents), 0)::bigint FROM trust.withdrawal_request
WHERE ledger_id = $1::uuid AND status = 'authorized'`, string(env.ledger)).Scan(&authorized, &held)
	}); err != nil {
		t.Fatal(err)
	}
	if authorized != ok || held != balance {
		t.Fatalf("authorized = %d holding %d, want %d holding %d", authorized, held, ok, balance)
	}
	b, err := env.store.Balance(context.Background(), env.ledger)
	if err != nil {
		t.Fatal(err)
	}
	if b.AvailableCents != 0 || b.PostedCents != balance {
		t.Fatalf("balance = %+v, want available 0 and posted %d (no posting)", b, balance)
	}
}

type itDir struct{}

func (itDir) User(_ context.Context, id string) (signatory.User, error) {
	return signatory.User{ID: id, Active: true, Licensee: true}, nil
}

type itSeal struct{}

func (itSeal) Seal(_ context.Context, p []byte) ([]byte, error) {
	return append([]byte("sealed:"), p...), nil
}

type itSigners struct{}

func (itSigners) IsSignatory(ctx context.Context, tx pgx.Tx, acct trust.AccountID, u string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM trust.signatory WHERE account_id = $1::uuid AND user_id = $2::uuid AND valid_to IS NULL)`,
		string(acct), u).Scan(&ok)
	return ok, err
}

type itFirm struct{}

func (itFirm) SolePractitioner(context.Context) (bool, error) { return false, nil }

type itAudit struct{}

func (itAudit) Record(context.Context, pgx.Tx, string, string, string) error { return nil }
