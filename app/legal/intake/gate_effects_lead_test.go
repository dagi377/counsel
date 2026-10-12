package intake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatestest"
	"github.com/dagi377/counsel/kernel/registry"
)

// rule: ARC-I-012
// rule: ARC-I-016
// rule: ARC-I-014
func TestIntakeLeadGateEffects(t *testing.T) {
	type run func(context.Context, *gatestest.Tx, registry.Approved) error
	consult := func(ctx context.Context, tx *gatestest.Tx, g registry.Approved) error {
		return consultOutcome(ctx, tx, g, gatestest.Now)
	}
	fee := func(ctx context.Context, tx *gatestest.Tx, g registry.Approved) error {
		return feeWaiver(ctx, tx, g, gatestest.Now)
	}
	opened := 0
	open := func(ctx context.Context, tx *gatestest.Tx, g registry.Approved) error {
		return openMatter(ctx, tx, g, gatestest.Now, func(context.Context, registry.Approved) error { opened++; return nil })
	}
	form := func(ctx context.Context, tx *gatestest.Tx, g registry.Approved) error {
		return publicWording(ctx, tx, g, gatestest.Now)
	}
	const (
		consultOK = `{"lead_id":"l-1","outcome":"engage"}`
		waiveOK   = `{"lead_id":"l-1","amount_cents":15000,"action":"waive"}`
		refundOK  = `{"lead_id":"l-1","amount_cents":15000,"action":"refund"}`
		openOK    = `{"lead_id":"l-1","responsible_lawyer_id":"u-9","matter_id":"m-1","number":"2026-001","short_name":"Estate"}`
		formOK    = `{"form_id":"f-1","version":3}`
	)
	lead := func(decision, converted string) [][]any {
		return [][]any{{"c-1", decision, converted, "pa-1"}}
	}
	gt := gatestest.Gate
	L, P, A := gatestest.Lawyer, gatestest.Prep, gatestest.Agent
	tests := []struct {
		name     string
		fn       run
		g        registry.Approved
		rows     []int64
		scans    [][]any
		wantSQL  string
		wantErr  error
		wantOpen int
	}{
		{"G3 outcome recorded", consult, gt(GateConsultOutcome, consultOK, A, L), nil, nil, "SET outcome = $2", nil, 0},
		{"G3 repeat re-matches", consult, gt(GateConsultOutcome, consultOK, A, L), []int64{1}, nil, "outcome = $2 AND signed_off_by", nil, 0},
		{"G3 decided otherwise fails closed", consult, gt(GateConsultOutcome, consultOK, A, L), []int64{0}, nil, "", gates.ErrNotApprovable, 0},
		{"G3 agent never approves", consult, gt(GateConsultOutcome, consultOK, P, A), nil, nil, "", gates.ErrAgentApproves, 0},
		{"G3 preparer never approves", consult, gt(GateConsultOutcome, consultOK, A, P), nil, nil, "", gates.ErrPreparerApproves, 0},
		{"G-fee waive", fee, gt(GateFeeWaiver, waiveOK, P, L), nil, nil, "fee_status = 'waived'", nil, 0},
		{"G-fee waive repeat matches waived", fee, gt(GateFeeWaiver, waiveOK, P, L), []int64{1}, nil, "IN ('due','waived')", nil, 0},
		{"G-fee refund", fee, gt(GateFeeWaiver, refundOK, P, L), nil, nil, "payment_status = 'refunded'", nil, 0},
		{"G-fee wrong amount fails closed", fee, gt(GateFeeWaiver, waiveOK, P, L), []int64{0}, nil, "", gates.ErrNotApprovable, 0},
		{"G-fee agent never approves", fee, gt(GateFeeWaiver, waiveOK, P, A), nil, nil, "", gates.ErrAgentApproves, 0},
		{"G-fee preparer never approves", fee, gt(GateFeeWaiver, waiveOK, P, P), nil, nil, "", gates.ErrPreparerApproves, 0},
		{"G-open opens through matter.opening", open, gt(GateOpenMatter, openOK, A, L), nil, lead("accept", ""), "converted_matter_id = $2", nil, 1},
		{"G-open repeat is a no-op", open, gt(GateOpenMatter, openOK, A, L), nil, lead("accept", "m-1"), "", nil, 0},
		{"G-open declined lead fails closed", open, gt(GateOpenMatter, openOK, A, L), nil, lead("decline", ""), "", gates.ErrNotApprovable, 0},
		{"G-open agent never approves", open, gt(GateOpenMatter, openOK, P, A), nil, nil, "", gates.ErrAgentApproves, 0},
		{"G-open preparer never approves", open, gt(GateOpenMatter, openOK, A, P), nil, nil, "", gates.ErrPreparerApproves, 0},
		{"G-form version current publishes", form, gt(GatePublicWording, formOK, P, L), nil, nil, "row_version = $4", nil, 0},
		{"G-form stale version is a conflict", form, gt(GatePublicWording, formOK, P, L), []int64{0, 1}, nil, "row_version <> $2", gates.ErrStale, 0},
		{"G-form not draft fails closed", form, gt(GatePublicWording, formOK, P, L), []int64{0, 0}, nil, "", gates.ErrNotApprovable, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opened = 0
			tx := &gatestest.Tx{Rows: tt.rows, Scans: tt.scans}
			err := tt.fn(context.Background(), tx, tt.g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL != "" && !tx.Ran(tt.wantSQL) {
				t.Fatalf("ran %q, want %q", tx.SQLs, tt.wantSQL)
			}
			if opened != tt.wantOpen {
				t.Fatalf("matter.opening ran %d times, want %d", opened, tt.wantOpen)
			}
		})
	}
}
