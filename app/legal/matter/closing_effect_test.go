package matter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestClosingEffect(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	const ok = `{"matter_id":"m-1","trust_balance_cents":0}`
	tests := []struct {
		name     string
		payload  string
		prep     registry.Principal
		approver registry.Principal
		rows     []int64
		wantSQL  string
		wantErr  error
	}{
		{"matter closed, dated in Toronto", ok, efAgent, efLawyer, nil, "status = 'closed'", nil},
		{"repeat leaves a closed matter", ok, efAgent, efLawyer, []int64{1}, "'open','closing','closed'", nil},
		// rule: ARC-I-025
		{"trust money held never closes", `{"matter_id":"m-1","trust_balance_cents":1}`, efAgent, efLawyer, nil, "", gates.ErrNotApprovable},
		{"a ledger not at zero fails closed", ok, efAgent, efLawyer, []int64{0}, "trust.ledger_balance", gates.ErrNotApprovable},
		{"no trust balance in payload", `{"matter_id":"m-1"}`, efAgent, efLawyer, nil, "", gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", ok, efPrep, efAgent, nil, "", gates.ErrAgentApproves},
		{"preparer never approves", ok, efAgent, efPrep, nil, "", gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &effectExec{rows: tt.rows}
			err := closeMatter(context.Background(), tx, efGate(GateClosing, tt.payload, tt.prep, tt.approver), efNow, loc)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL == "" {
				if len(tx.sqls) != 0 {
					t.Fatalf("ran %q, want nothing", tx.sqls)
				}
				return
			}
			if !strings.Contains(tx.sqls[0], tt.wantSQL) {
				t.Fatalf("ran %q, want %q", tx.sqls[0], tt.wantSQL)
			}
			// 23:30 UTC on Oct 11 is still Oct 11 in Toronto.
			if got := tx.args[0][2]; got != "2026-10-11" {
				t.Fatalf("close date = %v, want 2026-10-11", got)
			}
		})
	}
}
