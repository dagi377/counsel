//go:build integration

package bill

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/matter/mattertest"
	"github.com/dagi377/counsel/kernel/registry"
)

// pgFeesBook is a test fees book in the throwaway database. 0001_init.sql has no fees book
// table yet (docs/QUESTIONS.md, T-019.1f); this one shows the row lives and dies with the
// gate tx.
type pgFeesBook struct{}

func (pgFeesBook) Append(ctx context.Context, tx pgx.Tx, e FeesBookEntry) error {
	_, err := tx.Exec(ctx, `INSERT INTO public.test_fees_book (number, bill_date, client_id, matter_id, total_cents, status)
VALUES ($1, $2::date, $3::uuid, $4::uuid, $5, $6)`,
		e.Number, e.BillDate.In(business).Format(time.DateOnly), e.ClientID, e.MatterID, e.TotalCents, string(e.Status))
	return err
}

type approveEnv struct {
	pool     *pgxpool.Pool
	preparer string
	lawyer   string
	matter   string
	client   string
}

func seedApproveEnv(t *testing.T) approveEnv {
	t.Helper()
	ctx := context.Background()
	env := approveEnv{pool: testPool(t)}
	err := pgx.BeginFunc(ctx, env.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TABLE public.test_fees_book (number text PRIMARY KEY, bill_date date NOT NULL,
client_id uuid NOT NULL, matter_id uuid NOT NULL, total_cents bigint NOT NULL, status text NOT NULL)`); err != nil {
			return err
		}
		var role, area string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for _, u := range []struct {
			name string
			id   *string
		}{{"prep", &env.preparer}, {"lawyer", &env.lawyer}} {
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id) VALUES ($1 || '@example.com', $1, $2::uuid) RETURNING id::text`,
				u.name, role).Scan(u.id); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO party.contact(kind, legal_name, display_name) VALUES ('person', 'C', 'C') RETURNING id::text`).Scan(&env.client); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO matter.practice_area(name) VALUES ('Litigation') RETURNING id::text`).Scan(&area); err != nil {
			return err
		}
		var err error
		env.matter, err = mattertest.Seed(ctx, tx, "2026-0001", "M", env.client, area, env.lawyer)
		return err
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return env
}

// pendingWithGate inserts a pending bill prepared by env.preparer and its approved
// billing.bill_approval gate, and returns the gate as the kernel passes it to the effect.
func (env approveEnv) pendingWithGate(t *testing.T) registry.Approved {
	t.Helper()
	ctx := context.Background()
	var billID, gateID string
	err := pgx.BeginFunc(ctx, env.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO billing.bill (matter_id, payer_id, status, fees_cents, tax_cents, total_cents,
place_of_supply_province, created_by) VALUES ($1::uuid, $2::uuid, 'pending_approval', 100000, 13000, 113000, 'ON', $3::uuid)
RETURNING id::text`, env.matter, env.client, env.preparer).Scan(&billID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO gate.approval_request(gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, status, expires_at)
VALUES ('billing.bill_approval', 'G-B2', 'billing.bill', $1::uuid, '\x00'::bytea, $2::uuid, 'approved', now() + interval '1 day') RETURNING id::text`,
			billID, env.preparer).Scan(&gateID)
	})
	if err != nil {
		t.Fatalf("seed bill: %v", err)
	}
	payload, _ := json.Marshal(ApprovalPayload{BillID: billID, MatterID: env.matter, TotalCents: 113000, DeliveryMethod: "email"})
	return registry.Approved{GateID: gateID, Kind: GateBillApproval, Payload: payload,
		Preparer:  registry.Principal{Kind: registry.PrincipalUser, UserID: env.preparer},
		Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: env.lawyer}}}
}

func (env approveEnv) state(t *testing.T, billID string) (status, number string, rows int) {
	t.Helper()
	ctx := context.Background()
	if err := env.pool.QueryRow(ctx, `SELECT status, coalesce(number, '') FROM billing.bill WHERE id = $1::uuid`, billID).Scan(&status, &number); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM public.test_fees_book WHERE number = $1`, number).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return status, number, rows
}

var errRollback = errors.New("roll back after the effect")

// rule: BIL-C-120
func TestBIL_C_120_ApproveViaGateInTx(t *testing.T) {
	env := seedApproveEnv(t)
	ctx := context.Background()
	eff := &ApproveEffect{Store: PGStore{}, FeesBook: pgFeesBook{}, Prefix: "INV",
		Now: func() time.Time { return time.Date(2026, 9, 15, 15, 0, 0, 0, time.UTC) }}
	billOf := func(g registry.Approved) string {
		var p ApprovalPayload
		if err := json.Unmarshal(g.Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p.BillID
	}
	tests := []struct {
		name       string
		rollback   bool
		wantStatus string
		wantNumber string
	}{
		{"rolled-back effect leaves no number and no row", true, "pending_approval", ""},
		{"committed effect takes the first number", false, "approved", "INV-2026-0001"},
		{"next approval is consecutive", false, "approved", "INV-2026-0002"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := env.pendingWithGate(t)
			err := pgx.BeginFunc(ctx, env.pool, func(tx pgx.Tx) error {
				if err := eff.Apply(ctx, tx, g); err != nil {
					return err
				}
				if tc.rollback {
					return errRollback
				}
				return nil
			})
			if tc.rollback != errors.Is(err, errRollback) || (!tc.rollback && err != nil) {
				t.Fatalf("apply: %v", err)
			}
			status, number, rows := env.state(t, billOf(g))
			wantRows := 0
			if tc.wantNumber != "" {
				wantRows = 1
			}
			if status != tc.wantStatus || number != tc.wantNumber || rows != wantRows {
				t.Fatalf("bill = %s %q, fees book rows %d; want %s %q, %d", status, number, rows,
					tc.wantStatus, tc.wantNumber, wantRows)
			}
		})
	}
}
