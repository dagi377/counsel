package credit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// secondRig is a writeOffRig whose gatefake runs both write-off effects. responsible is the
// bill's responsible lawyer, who gives the G-B5 approval. The firm threshold is $500.00, so
// the BIL-X-014 write-off of $590.00 needs a second approval.
type secondRig struct {
	writeOffRig
	second *WriteOffSecondEffect
	gateID gate.GateID // the pending second gate
}

func newSecondRig(t *testing.T, responsible string) secondRig {
	t.Helper()
	r := secondRig{writeOffRig: newWriteOffRig(t, 59000, SecondApprovalConfig{WriteOffSecondApprovalCents: 50000})}
	bills := r.e.Bills.(memBills)
	b := bills["b1"]
	b.ResponsibleUser = responsible
	bills["b1"] = b
	// rule: BIL-C-085
	// Only the owner role holds billing.writeoff_second_approve and is a managing lawyer.
	r.gates = gatefake.New(clockfake.New(effectNow, toronto).Now, gatefake.Caps{
		"lawyer": {"billing.credit"},
		"clerk":  {"billing.credit"},
		"owner":  {"billing.credit", "billing.writeoff_second_approve"},
		"owner2": {"billing.credit", "billing.writeoff_second_approve"},
	})
	r.e.Gates = r.gates
	r.second = &WriteOffSecondEffect{WriteOff: r.e, Second: SecondApproval{ManagingLawyer: func(id string) bool {
		return id == "owner" || id == "owner2"
	}}}
	r.registerKinds(t, map[string]registry.EffectFunc{GateKindCreditNote: r.e.Apply, GateKindWriteOffSecond: r.second.Apply})
	if err := r.e.Apply(context.Background(), nil, gb5Approved(59000, user(responsible))); err != nil {
		t.Fatalf("G-B5 effect: %v", err)
	}
	r.gateID = gate.GateID(r.store.awaiting["w1"])
	if r.gateID == "" || len(r.ledger.posts) != 0 {
		t.Fatalf("G-B5 effect: second gate %q, posts %v; want a pending gate and no post", r.gateID, r.ledger.posts)
	}
	return r
}

func (r secondRig) decide(t *testing.T, v gate.Verdict, by string) error {
	t.Helper()
	g, err := r.gates.Get(context.Background(), r.gateID)
	if err != nil {
		t.Fatal(err)
	}
	reason := ""
	if v != gate.VerdictApprove {
		reason = "not uncollectible"
	}
	return r.gates.Decide(context.Background(), nil, r.gateID, g.Version,
		gate.Decision{Verdict: v, Reason: reason, PayloadHash: g.PayloadHash, MFAVerifiedAt: effectNow}, user(by))
}

// rule: BIL-C-085
// rule: BIL-C-205
func TestBIL_C_085_SecondApprovalCompletesWriteOff(t *testing.T) {
	tests := []struct {
		name        string
		responsible string // G-B5 approver
		verdict     gate.Verdict
		by          string
		wantErr     error
		wantStatus  gate.Status
	}{
		{"BIL-X-014: owner approves second, adjustment $65.00", "lawyer", gate.VerdictApprove, "owner", nil, gate.StatusApproved},
		{"owner who gave G-B5 cannot give the second approval", "owner", gate.VerdictApprove, "owner", gate.ErrSelfApproval, gate.StatusPending},
		{"another owner completes after an owner's G-B5", "owner", gate.VerdictApprove, "owner2", nil, gate.StatusApproved},
		{"responsible lawyer without the owner role cannot approve", "owner", gate.VerdictApprove, "lawyer", gate.ErrNotAuthorized, gate.StatusPending},
		{"preparer of the write-off cannot approve", "lawyer", gate.VerdictApprove, "clerk", gate.ErrNotAuthorized, gate.StatusPending},
		{"rejection leaves the write-off draft", "lawyer", gate.VerdictReject, "owner", nil, gate.StatusRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSecondRig(t, tt.responsible)
			err := r.decide(t, tt.verdict, tt.by)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("decide err = %v, want %v", err, tt.wantErr)
			}
			g, err := r.gates.Get(context.Background(), r.gateID)
			if err != nil {
				t.Fatal(err)
			}
			if g.Status != tt.wantStatus {
				t.Fatalf("gate status = %s, want %s", g.Status, tt.wantStatus)
			}
			w := r.store.w["w1"]
			if tt.wantStatus != gate.StatusApproved {
				if w.Status != StatusDraft || len(r.ledger.posts) != 0 {
					t.Fatalf("write-off %s, posts %v; want draft and no post", w.Status, r.ledger.posts)
				}
				return
			}
			// rule: BIL-C-205
			if len(r.ledger.posts) != 1 || r.ledger.posts[0].TaxCents != 6500 || r.ledger.posts[0].BillID != "b1" ||
				!r.ledger.posts[0].On.Equal(effectBizDate) {
				t.Fatalf("posts = %+v, want one adjustment of 6500 on %v", r.ledger.posts, effectBizDate)
			}
			if w.Status != StatusApproved || w.ApprovedBy != tt.responsible {
				t.Fatalf("write-off = %+v, want approved by %s", w, tt.responsible)
			}
			// Replay of the same approved gate is a no-op: no second post.
			replay := registry.Approved{GateID: string(g.ID), Kind: g.Kind, Version: g.Version, Subject: g.Subject,
				Payload: g.Payload, Preparer: g.Preparer, Approvers: g.Approvers}
			if err := r.second.Apply(context.Background(), nil, replay); err != nil {
				t.Fatalf("replay: %v", err)
			}
			if len(r.ledger.posts) != 1 {
				t.Fatalf("replay posted again: %+v", r.ledger.posts)
			}
		})
	}
}

// rule: BIL-C-085
func TestBIL_C_085_SecondEffectChecksApprovers(t *testing.T) {
	payload := func(p SecondApprovalPayload) json.RawMessage {
		b, _ := json.Marshal(p)
		return b
	}
	valid := SecondApprovalPayload{WriteOffID: "w1", BillID: "b1", AmountCents: 59000, FirstApprover: "lawyer"}
	agent := gate.Principal{Kind: gate.PrincipalAgent, RunID: "run1", ActingFor: "owner"}
	tests := []struct {
		name     string
		payload  SecondApprovalPayload
		preparer gate.Principal
		approver gate.Principal
		wantErr  error
	}{
		{"owner approves", valid, user("lawyer"), user("owner"), nil},
		{"agent never approves", valid, user("lawyer"), agent, ErrAgentApproval},
		{"first approver never approves twice", valid, user("lawyer"), user("lawyer"), ErrSelfApproval},
		{"write-off preparer never approves", valid, user("lawyer"), user("clerk"), ErrSelfApproval},
		{"a non-owner is not a managing lawyer", valid, user("lawyer"), user("other"), ErrSecondApprovalRequired},
		{"gate not prepared by the first approver", valid, user("owner2"), user("owner"), ErrInvalidState},
		{"amount differs from the write-off", func() SecondApprovalPayload { p := valid; p.AmountCents = 1; return p }(), user("lawyer"), user("owner"), ErrInvalidState},
		{"first approver is not the responsible lawyer", func() SecondApprovalPayload { p := valid; p.FirstApprover = "owner2"; return p }(), user("owner2"), user("owner"), ErrNotResponsible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSecondRig(t, "lawyer")
			err := r.second.Apply(context.Background(), nil, registry.Approved{
				GateID: "g2", Kind: GateKindWriteOffSecond, Version: 1, Subject: gate.Subject{Kind: SubjectWriteOff, ID: "w1"},
				Payload: payload(tt.payload), Preparer: tt.preparer, Approvers: []gate.Principal{tt.approver},
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			wantPosts := 0
			if tt.wantErr == nil {
				wantPosts = 1
			}
			if len(r.ledger.posts) != wantPosts {
				t.Fatalf("posts = %v, want %d", r.ledger.posts, wantPosts)
			}
		})
	}
}
