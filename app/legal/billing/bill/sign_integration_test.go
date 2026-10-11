//go:build integration

package bill

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// approvedBill approves a fresh pending bill through the G-B2 effect and returns its id.
func (env approveEnv) approvedBill(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	g := env.pendingWithGate(t)
	eff := &ApproveEffect{Store: PGStore{}, FeesBook: PGFeesBook{}, Journal: PGStore{}, Ledger: env.ledger(), Prefix: "INV",
		Now: func() time.Time { return time.Date(2026, 9, 15, 15, 0, 0, 0, time.UTC) }}
	if err := pgx.BeginFunc(ctx, env.pool, func(tx pgx.Tx) error { return eff.Apply(ctx, tx, g) }); err != nil {
		t.Fatalf("approve: %v", err)
	}
	var p ApprovalPayload
	if err := json.Unmarshal(g.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p.BillID
}

// refused runs sql (with $1 = the bill id) in a rolled-back tx and wants a DM-041 refusal.
func (env approveEnv) refused(t *testing.T, sql, billID string) {
	t.Helper()
	ctx := context.Background()
	err := pgx.BeginFunc(ctx, env.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sql, billID); err != nil {
			return err
		}
		return errRollback
	})
	if err == nil || errors.Is(err, errRollback) || !strings.Contains(err.Error(), "DM-041") {
		t.Fatalf("%s: err = %v, want DM-041", sql, err)
	}
}

type signRunner struct{ env approveEnv }

func (r signRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, r.env.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
}

// rule: BIL-L-001
// On Postgres an approved bill is signed once (signed_by, signed_at, signature_hash together)
// and every other frozen column stays locked before and after the signature (DM-041,
// migrations/5620_bill_sign_after_approval.sql).
func TestBIL_L_001_SignApprovedBillOnPostgres(t *testing.T) {
	env := seedApproveEnv(t)
	ctx := context.Background()
	billID := env.approvedBill(t)
	run := signRunner{env}
	svc := NewPGService(run, time.Now)
	sig := Signature{SignedBy: env.lawyer, SignedAt: time.Now().Add(-time.Minute), Hash: "sha256:" + strings.Repeat("ab", 32)}

	frozen := []struct{ name, sql string }{
		{"fees", `UPDATE billing.bill SET fees_cents = fees_cents + 1, total_cents = total_cents + 1 WHERE id = $1::uuid`},
		{"tax", `UPDATE billing.bill SET tax_cents = tax_cents + 1, total_cents = total_cents + 1 WHERE id = $1::uuid`},
		{"discount", `UPDATE billing.bill SET discount_cents = 1, total_cents = total_cents - 1 WHERE id = $1::uuid`},
		{"number", `UPDATE billing.bill SET number = 'X-1' WHERE id = $1::uuid`},
		{"payer", `UPDATE billing.bill SET payer_id = matter_id WHERE id = $1::uuid`},
		{"tax breakdown", `UPDATE billing.bill SET tax_breakdown = '[]' WHERE id = $1::uuid`},
		{"issue date", `UPDATE billing.bill SET issue_date = issue_date - 1 WHERE id = $1::uuid`},
		{"due date", `UPDATE billing.bill SET due_date = issue_date + 60 WHERE id = $1::uuid`},
		{"approver", `UPDATE billing.bill SET approved_by = created_by WHERE id = $1::uuid`},
		{"approved at", `UPDATE billing.bill SET approved_at = approved_at - interval '1 day' WHERE id = $1::uuid`},
		{"signature ref", `UPDATE billing.bill SET signature_ref = 'r' WHERE id = $1::uuid`},
		{"interest rate", `UPDATE billing.bill SET interest_rate_ppm = 30000 WHERE id = $1::uuid`},
		{"place of supply", `UPDATE billing.bill SET place_of_supply_province = 'BC' WHERE id = $1::uuid`},
		{"place of supply detail", `UPDATE billing.bill SET place_of_supply = '{}' WHERE id = $1::uuid`},
	}
	t.Run("unsigned approved bill keeps every other column frozen", func(t *testing.T) {
		for _, f := range frozen {
			t.Run(f.name, func(t *testing.T) { env.refused(t, f.sql, billID) })
		}
	})
	partial := []struct{ name, sql string }{
		{"signer only", `UPDATE billing.bill SET signed_by = created_by WHERE id = $1::uuid`},
		{"hash only", `UPDATE billing.bill SET signature_hash = '\xab' WHERE id = $1::uuid`},
		{"signature with an amount", `UPDATE billing.bill SET signed_by = created_by, signed_at = now(), signature_hash = '\xab',
       fees_cents = fees_cents + 1, total_cents = total_cents + 1 WHERE id = $1::uuid`},
	}
	t.Run("partial or mixed signature refused", func(t *testing.T) {
		for _, p := range partial {
			t.Run(p.name, func(t *testing.T) { env.refused(t, p.sql, billID) })
		}
	})
	t.Run("Sign sets the signature once", func(t *testing.T) {
		if err := run.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return svc.Sign(ctx, tx, ID(billID), sig) }); err != nil {
			t.Fatalf("sign: %v", err)
		}
		b, err := svc.Get(ctx, ID(billID))
		if err != nil {
			t.Fatal(err)
		}
		if b.SignedBy != env.lawyer || b.SignatureHash != sig.Hash || b.Status != StatusApproved {
			t.Fatalf("bill after sign = %s %q %q, want approved, %q %q", b.Status, b.SignedBy, b.SignatureHash, env.lawyer, sig.Hash)
		}
	})
	t.Run("signed bill keeps every other column frozen", func(t *testing.T) {
		for _, f := range frozen {
			t.Run(f.name, func(t *testing.T) { env.refused(t, f.sql, billID) })
		}
	})
	resign := []struct{ name, sql string }{
		{"new signer", `UPDATE billing.bill SET signed_by = created_by WHERE id = $1::uuid`},
		{"new hash", `UPDATE billing.bill SET signature_hash = '\xcd' WHERE id = $1::uuid`},
		{"cleared", `UPDATE billing.bill SET signed_by = NULL, signed_at = NULL, signature_hash = NULL WHERE id = $1::uuid`},
	}
	t.Run("signature is never rewritten", func(t *testing.T) {
		for _, r := range resign {
			t.Run(r.name, func(t *testing.T) { env.refused(t, r.sql, billID) })
		}
		err := run.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return svc.Sign(ctx, tx, ID(billID), sig) })
		if !errors.Is(err, ErrBillGuard) {
			t.Fatalf("second sign: %v, want ErrBillGuard", err)
		}
	})
}
