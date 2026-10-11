package docs_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// F-DOC-01 starts on every document a matter receives, whatever the channel, and acts for the
// event's user.
func TestFDOC01_TriggerBinding(t *testing.T) {
	b, err := docs.IngestDocumentBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, docs.FeatureIngestDocument},
		{"events", strings.Join(b.Events, ","), docs.EventDocumentUploaded},
		{"no schedule", b.Cadence, trigger.CadenceNone},
		{"acts for the event's user", b.ActingFor, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-039
func TestFDOC01_WritePathBothGates(t *testing.T) {
	w, calls, run := dochIngestRun(t, dochIngestLines, dochIngestDue, docs.GateFilingPrivilege)
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// Every tool of the Def runs its real handler, read_document included.
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", w.fd.StandIns, run.StandIns)
	}
	if want := docToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt != 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolProposeDeadline {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before propose_deadline", run.PlanAt, run.FirstWrite, run.Tools)
	}
	// The filing review is a draft the acting user prepared; nothing is filed.
	if len(w.drafts.drafts) != 1 || w.drafts.drafts[0].PreparedBy != agenttest.FeatureActingFor {
		t.Fatalf("drafts = %+v, want one prepared by %s", w.drafts.drafts, agenttest.FeatureActingFor)
	}
	if len(run.GateIDs) != 2 {
		t.Fatalf("gates = %v, want G7 and G-D1", run.GateIDs)
	}
	// Each gate is pending for the acting user as preparer: the agent principal deciding is
	// 403, the acting user deciding is ErrSelfApproval (403), another lawyer may decide.
	for i, kind := range []string{docs.GateCalConfirm, docs.GateFilingPrivilege} {
		t.Run(kind, func(t *testing.T) { docCheckGate(t, w.fd, run.GateIDs[i], kind) })
	}
}

// rule: ARC-I-035
// rule: ARC-U-027
// submit_for_approval opens only the Def's gate kinds, and a due date the run never read is
// refused before any deadline is stored.
func TestFDOC01_WritePathRefused(t *testing.T) {
	tests := []struct {
		name       string
		dueOn      string
		filingKind string
		tool       string
		wantErr    error
		gates      int
	}{
		{"gate kind outside the Def", dochIngestDue, "docs.outgoing", agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed, 1},
		{"another documents gate kind", dochIngestDue, "docs.template_publish", agent.ToolSubmitForApproval, agenttools.ErrGateKindNotAllowed, 1},
		{"made-up due date", "2026-12-01", docs.GateFilingPrivilege, agent.ToolProposeDeadline, agenttools.ErrUnsourcedValue, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, run := dochIngestRun(t, dochIngestLines, tt.dueOn, tt.filingKind)
			if len(run.Refused) != 1 || run.Refused[0].Tool != tt.tool || !errors.Is(run.Refused[0].Err, tt.wantErr) {
				t.Fatalf("refused = %v, want %s refused with %v", run.Refused, tt.tool, tt.wantErr)
			}
			if len(run.GateIDs) != tt.gates {
				t.Fatalf("gates = %v, want %d", run.GateIDs, tt.gates)
			}
		})
	}
}

// rule: ARC-I-038
func TestFDOC01_WritePathInjection(t *testing.T) {
	w, calls, run := dochIngestRun(t, append(slices.Clone(dochIngestLines), dochIngestInjected), dochIngestDue, docs.GateFilingPrivilege)
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := docToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	if err := agenttest.QuotedOnly(run.Run, dochIngestInjected); err != nil {
		t.Fatal(err)
	}
	// Neither write input nor any gate payload carries the injected text.
	if err := docAbsent(context.Background(), w.fd, run, dochIngestInjected); err != nil {
		t.Fatal(err)
	}
}
