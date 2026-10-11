package trust_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// TestSourcedCents_ProductionWrap runs one draft through boot's production handler set
// (trubHandlers starts from boot.AgentHandlers). The wrap is per feature: a trust feature's
// draft with an invented cents figure is refused, while the same draft from a non-trust
// feature runs the default draft_document handler unchecked.
//
// rule: ARC-U-027
func TestSourcedCents_ProductionWrap(t *testing.T) {
	const invented = "Trust balance 480001 cents."
	nonTrust := trust.AnnualReportPrepDef()
	nonTrust.ID, nonTrust.Area, nonTrust.Fixture = "F-MAT-01", "matter", "testdata/ai/f-mat-01/non-trust-draft"
	tests := []struct {
		name    string
		trust   bool
		body    string
		refused bool
	}{
		{"trust feature, invented figure refused", true, invented, true},
		{"trust feature, sourced figure stored", true, trub06Body, false},
		{"non-trust feature, same invented figure stored", false, invented, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := trust.AnnualReportPrepDef()
			if !tt.trust {
				def = nonTrust
			}
			w := trubFeature(t, def, trub06Ledger)
			if len(w.fd.StandIns) != 0 {
				t.Fatalf("stand-ins %v: the production handlers must serve every tool", w.fd.StandIns)
			}
			calls := append(trubReads(w), trubDocumentWrites(t, w, "Trust worksheet", tt.body, trust.GateRegulatoryRelease)[:2]...)
			run := trubFire(t, w, calls, "fiscal year ended")
			if tt.refused {
				trubCheckRefused(t, w, run, agent.ToolDraftDocument, agenttools.ErrUnsourcedValue, 0)
				return
			}
			if len(run.Refused) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
				t.Fatalf("refused %v, drafts %d, tools %v, err %v; want none refused and one draft", run.Refused, len(w.fd.Seed.Store.Drafts), run.Tools, run.Err)
			}
		})
	}
}
