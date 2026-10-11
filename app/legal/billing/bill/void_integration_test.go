//go:build integration

package bill

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

type glLine struct {
	account string
	debit   int64
	credit  int64
}

func entryLines(t *testing.T, env approveEnv, id ledger.EntryID) []glLine {
	t.Helper()
	rows, err := env.pool.Query(context.Background(), `SELECT a.code, l.debit_cents, l.credit_cents
FROM gl.journal_line l JOIN gl.account a ON a.id = l.account_id WHERE l.entry_id = $1::uuid ORDER BY a.code, l.debit_cents, l.credit_cents`, string(id))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []glLine
	for rows.Next() {
		var l glLine
		if err := rows.Scan(&l.account, &l.debit, &l.credit); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// rule: BIL-C-084
func TestBIL_C_084_VoidViaGateReversesExactly(t *testing.T) {
	env := seedApproveEnv(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	ctx = clock.WithClock(ctx, clockfake.New(at, business))
	led := env.ledger()
	approve := &ApproveEffect{Store: PGStore{}, FeesBook: PGFeesBook{}, Journal: PGStore{}, Ledger: led, Prefix: "INV",
		Now: func() time.Time { return at }}

	// Approving the bill posts its BIL-J-003 entry; the void finds it by source.
	g := env.pendingWithGate(t)
	var p ApprovalPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil {
		t.Fatal(err)
	}
	billID := ID(p.BillID)
	var entry ledger.EntryID
	err := pgx.BeginFunc(ctx, env.pool, func(tx pgx.Tx) error {
		if err := approve.Apply(ctx, tx, g); err != nil {
			return err
		}
		var err error
		entry, err = PGStore{}.ApprovalEntry(ctx, tx, billID)
		return err
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	voidAt := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	vctx := clock.WithClock(ctx, clockfake.New(voidAt, business))
	void := &VoidEffect{Store: PGStore{}, Entries: PGStore{}, Ledger: led, Now: func() time.Time { return voidAt }}
	vg := registry.Approved{GateID: g.GateID, Kind: GateBillVoid,
		Preparer:  registry.Principal{Kind: registry.PrincipalUser, UserID: env.preparer},
		Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: env.lawyer}}}
	vg.Payload, _ = json.Marshal(VoidPayload{BillID: p.BillID, Reason: "duplicate"})

	tests := []struct {
		name       string
		rollback   bool
		wantStatus string
		wantRevs   int
	}{
		{"rolled-back void leaves the bill approved and no reversal", true, "approved", 0},
		{"committed void keeps the number and reverses once", false, "void", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := pgx.BeginFunc(vctx, env.pool, func(tx pgx.Tx) error {
				if err := void.Apply(vctx, tx, vg); err != nil {
					return err
				}
				if tc.rollback {
					return errRollback
				}
				return nil
			})
			if tc.rollback != errors.Is(err, errRollback) || (!tc.rollback && err != nil) {
				t.Fatalf("void: %v", err)
			}
			status, number, _ := env.state(t, p.BillID)
			if status != tc.wantStatus || number != "INV-2026-0001" {
				t.Fatalf("bill = %s %q, want %s with its number kept", status, number, tc.wantStatus)
			}
			var revs int
			var rev string
			var revDate time.Time
			if err := env.pool.QueryRow(ctx, `SELECT count(*), coalesce(max(id::text), ''), coalesce(max(entry_date), '1970-01-01'::date)
FROM gl.journal_entry WHERE reverses_id = $1::uuid`, string(entry)).Scan(&revs, &rev, &revDate); err != nil {
				t.Fatal(err)
			}
			if revs != tc.wantRevs {
				t.Fatalf("reversals = %d, want %d", revs, tc.wantRevs)
			}
			var credits int
			if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM billing.credit_note WHERE bill_id = $1::uuid`, p.BillID).Scan(&credits); err != nil {
				t.Fatal(err)
			}
			if credits != 0 {
				t.Fatalf("void wrote %d credit notes", credits)
			}
			if revs == 0 {
				return
			}
			if got := revDate.Format(time.DateOnly); got != "2026-10-08" {
				t.Fatalf("reversal dated %s, want the void date 2026-10-08", got)
			}
			orig := entryLines(t, env, entry)
			mirror := entryLines(t, env, ledger.EntryID(rev))
			if len(orig) != len(mirror) {
				t.Fatalf("reversal has %d lines, approval %d", len(mirror), len(orig))
			}
			want := map[string]int64{}
			for _, l := range orig {
				want[l.account] += l.debit - l.credit
			}
			var sum int64
			for _, l := range mirror {
				want[l.account] += l.debit - l.credit
				sum += l.debit - l.credit
			}
			if sum != 0 {
				t.Fatalf("reversal does not balance: %d", sum)
			}
			for acct, net := range want {
				if net != 0 {
					t.Fatalf("account %s nets %d after the void; want the exact negation", acct, net)
				}
			}
		})
	}
}
