package platform_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/gate"
)

// F-PLT-06 starts when a matter's stage changes, acting for the event's user; it is no
// scheduled job and has no screen action.
func TestFPLT06_TriggerBinding(t *testing.T) {
	d := platform.ClosingStarterDef()
	b, err := platform.ClosingStarterBinding()
	pltfCheckBinding(t, b, err, d, []string{matter.EventStageChanged}, trigger.CadenceNone, "")
}

// plt06Balance is the concluded matter's cleared trust balance, in cents; its ledger
// statement states it.
const plt06Balance = 250_000

// plt06Docs is the concluded matter's file: one closing report holding its work summary and
// its trust ledger balance. (One document keeps the run inside the driver's turn bound.)
var plt06Docs = [][]string{{
	"Closing report. The matter concluded on 2026-10-02.",
	"Unbilled work since the last account (cents): 180000.",
	"Trust ledger: balance held for the client (cents): 250000. No cheque outstanding.",
}}

const plt06RunID = "run-fplt06"

// plt06Writes are the run's writes on w's matter: the plan, the trust refund (G11), the final
// account drafted and sent to G-B2, and the closing checklist drafted and sent to closeKind.
func plt06Writes(t *testing.T, w pltfWorld, closeKind string) ([]pltfCall, pltkRefund) {
	t.Helper()
	r := pltkRefund{cents: plt06Balance, payee: w.clientID, purpose: "refund"}
	account, checklist := w.draftID("document"), fmt.Sprintf("draft-document-%d", len(w.docs)+2)
	submit := func(id, kind string) pltfCall {
		in, err := platform.ClosingSubmitInput(id, kind)
		if err != nil {
			// A kind outside the write path goes out as the model would send it.
			in = []byte(pltfJSON(t, map[string]string{"subject_type": "document", "subject_id": id, "gate_kind": kind}))
		}
		return pltfCall{agent.ToolSubmitForApproval, string(in)}
	}
	calls := append(pltkTrustWrites(t, w, r),
		pltfCall{agent.ToolDraftDocument, pltfJSON(t, map[string]string{"matter_id": w.matterID, "title": "Final account",
			"body_markdown": "Final account for the unbilled work since the last account, for review before delivery."})},
		submit(account, bill.GateBillApproval),
		pltfCall{agent.ToolDraftDocument, pltfJSON(t, map[string]string{"matter_id": w.matterID, "title": "Closing checklist and letter",
			"body_markdown": "Closing checklist: final account approved, trust balance returned, originals returned, file closed. Closing letter to the client."})},
		submit(checklist, closeKind),
	)
	return calls, r
}

// plt06Fire fires F-PLT-06 once, as the stage-change event would, over a concluded matter
// whose file holds docs; the closing checklist goes to closeKind.
func plt06Fire(t *testing.T, docs [][]string, closeKind string) (pltfWorld, agenttest.FeatureRun, []pltfCall, pltkRefund) {
	t.Helper()
	w := pltfFeature(t, platform.ClosingStarterDef(), "2026-0306", "Concluded Matter", docs...)
	w.fd.Seed.TrustBalance(w.matterID, plt06Balance)
	writes, r := plt06Writes(t, w, closeKind)
	calls := append(pltfReads(t, w, "closing"), writes...)
	model := pltfScript("F-PLT-06", "closing-starter", calls, "Drafted the closing steps for approval.")
	prompt := "matter " + w.matterID + " reached its concluded stage"
	return w, w.fd.Fire(context.Background(), model, plt06RunID, prompt), calls, r
}

// rule: ARC-I-035
// rule: ARC-I-039
// The run plans, then requests the trust refund (G11) and drafts the final account (G-B2) and
// the closing checklist (G13) on the matter, each gate pending and prepared for the acting
// user; only another lawyer decides any of them and no money moved.
func TestFPLT06_FromFeature(t *testing.T) {
	w, run, calls, r := plt06Fire(t, plt06Docs, matter.GateClosing)
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want every tool on its real handler", w.fd.StandIns, run.StandIns)
	}
	if want := pltfToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != agent.ToolPrepareTrustWithdrawal {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before %s", run.PlanAt, run.FirstWrite, run.Tools, agent.ToolPrepareTrustWithdrawal)
	}
	pltfCheckReads(t, w, run)
	drafts := w.fd.Seed.Store.Drafts
	if len(drafts) != 2 {
		t.Fatalf("drafts = %+v, want the final account and the closing checklist", drafts)
	}
	for _, d := range drafts {
		if d.Kind != "document" || d.PreparedBy != agenttest.FeatureActingFor || d.MatterID != w.matterID {
			t.Fatalf("draft = %+v, want a document on %s prepared by %s", d, w.matterID, agenttest.FeatureActingFor)
		}
	}
	if len(run.GateIDs) != 3 {
		t.Fatalf("gates = %v, want G11, G-B2 and G13", run.GateIDs)
	}
	// G11: the withdrawal is requested only; the balance is untouched.
	pltkCheckWithdrawal(t, w, run, run.GateIDs[0], r, plt06Balance)
	for i, kind := range []string{bill.GateBillApproval, matter.GateClosing} {
		t.Run(kind, func(t *testing.T) {
			pltkCheckKernelGate(t, w.fd, run.GateIDs[i+1], kind)
			g, err := w.fd.Seed.Gates.Get(context.Background(), gate.GateID(run.GateIDs[i+1]))
			if err != nil || g.Subject.ID != drafts[i].ID {
				t.Fatalf("gate %s subject = %+v (%v), want draft %s", kind, g.Subject, err, drafts[i].ID)
			}
		})
	}
}

// rule: ARC-I-035
// The tool-gap kinds open only on a draft the run made, and submit_for_approval refuses any
// kind outside the Def; the write path's own input builder refuses it first.
func TestFPLT06_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{"platform.portal_message", "platform.skill_autonomy"} {
		t.Run(kind, func(t *testing.T) {
			if _, err := platform.ClosingSubmitInput("draft-document-3", kind); !errors.Is(err, platform.ErrBadClosingItem) {
				t.Fatalf("ClosingSubmitInput = %v, want ErrBadClosingItem", err)
			}
			w, run, _, _ := plt06Fire(t, plt06Docs, kind)
			last := run.Refused
			if len(last) != 1 || last[0].Tool != agent.ToolSubmitForApproval || !errors.Is(last[0].Err, agenttools.ErrGateKindNotAllowed) {
				t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", last)
			}
			// G11 and G-B2 opened before the refused call; nothing opened for the bad kind.
			if len(run.GateIDs) != 2 || len(w.fd.Seed.Store.Drafts) != 2 {
				t.Fatalf("gates %v, drafts %d; want two gates and both drafts", run.GateIDs, len(w.fd.Seed.Store.Drafts))
			}
		})
	}
	if _, err := platform.ClosingSubmitInput("", bill.GateBillApproval); !errors.Is(err, platform.ErrBadClosingItem) {
		t.Fatalf("empty draft id = %v, want ErrBadClosingItem", err)
	}
}

// rule: ARC-I-038
func TestFPLT06_Injection(t *testing.T) {
	docs := [][]string{append(slices.Clone(plt06Docs[0]), pltkInjected)}
	w, run, calls, _ := plt06Fire(t, docs, matter.GateClosing)
	pltkCheckInjection(t, w, run, calls, pltkInjected)
}
