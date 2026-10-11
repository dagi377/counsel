package timekeeping

import (
	"errors"
	"testing"
)

func TestCheckTransition(t *testing.T) {
	owner := Actor{UserID: "u1"}
	other := Actor{UserID: "u2"}
	agent := Actor{UserID: "u1", Agent: true}
	system := Actor{}
	tests := []struct {
		name     string
		from, to State
		by       Actor
		wantGate Gate
		wantErr  error
	}{
		{"owner accepts suggestion at G9", StateSuggested, StateAccepted, owner, GateG9, nil},
		{"agent suggestion never auto-accepted", StateSuggested, StateAccepted, agent, GateNone, ErrAgentForbidden},
		{"non-owner cannot accept", StateSuggested, StateAccepted, other, GateNone, ErrNotOwner},
		{"reject suggestion", StateSuggested, StateRejected, owner, GateNone, nil},
		{"draft to accepted by hand", StateDraft, StateAccepted, owner, GateNone, nil},
		{"supervisor approves at G9c", StateSubmitted, StateApproved, other, GateG9c, nil},
		{"self approval refused", StateSubmitted, StateApproved, owner, GateNone, ErrSelfApproval},
		{"system auto-approves", StateAccepted, StateApproved, system, GateNone, nil},
		{"user cannot auto-approve", StateAccepted, StateApproved, owner, GateNone, ErrBadTransition},
		{"agent builds draft", StateApproved, StateOnDraftBill, agent, GateNone, nil},
		{"agent cannot bill", StateOnDraftBill, StateBilled, agent, GateNone, ErrAgentForbidden},
		{"bill approved at G-B2", StateOnDraftBill, StateBilled, other, GateGB2, nil},
		{"suggested straight to billed", StateSuggested, StateBilled, owner, GateNone, ErrBadTransition},
		{"billed to written off", StateBilled, StateWrittenOff, owner, GateNone, ErrBadTransition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := TimeEntry{ID: "e1", UserID: "u1", State: tt.from}
			g, err := CheckTransition(e, tt.to, tt.by)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if g != tt.wantGate {
				t.Fatalf("gate = %q, want %q", g, tt.wantGate)
			}
		})
	}
}

// rule: BIL-I-007
func TestBIL_I_007(t *testing.T) {
	tests := []struct {
		name    string
		e       TimeEntry
		bill    string
		wantErr error
	}{
		{"approved entry attaches", TimeEntry{ID: "e1", State: StateApproved}, "b1", nil},
		{"second draft skips entry on another bill", TimeEntry{ID: "e1", State: StateOnDraftBill, BillID: "b1"}, "b2", ErrBilledLocked},
		{"same bill again is idempotent", TimeEntry{ID: "e1", State: StateOnDraftBill, BillID: "b1"}, "b1", nil},
		{"billed entry locked", TimeEntry{ID: "e1", State: StateBilled, BillID: "b1"}, "b2", ErrBilledLocked},
		{"unapproved entry", TimeEntry{ID: "e1", State: StateAccepted}, "b1", ErrBadTransition},
		{"missing bill id", TimeEntry{ID: "e1", State: StateApproved}, "", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckAttach(tt.e, tt.bill); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: BIL-C-014
func TestBIL_C_014(t *testing.T) {
	lawyer := Actor{UserID: "lw"}
	ok := WriteUp{EntryID: "e1", AmountCents: 20000, ReasonCode: "urgency premium per retainer s.4", LineKind: "hourly", ProposedBy: lawyer, ResponsibleUser: "lw"}
	with := func(f func(*WriteUp)) WriteUp { w := ok; f(&w); return w }
	tests := []struct {
		name    string
		w       WriteUp
		wantErr error
	}{
		{"+$200.00 with reason shown", ok, nil},
		{"without reason rejected", with(func(w *WriteUp) { w.ReasonCode = "" }), ErrInvalid},
		{"contingency line", with(func(w *WriteUp) { w.LineKind = LineContingency }), ErrInvalid},
		{"flat line", with(func(w *WriteUp) { w.LineKind = LineFlat }), ErrInvalid},
		{"raises minutes", with(func(w *WriteUp) { w.AdjustsMinutes = true }), ErrInvalid},
		{"raises rate", with(func(w *WriteUp) { w.AdjustsRateCents = true }), ErrInvalid},
		{"zero amount", with(func(w *WriteUp) { w.AmountCents = 0 }), ErrInvalid},
		{"not responsible lawyer", with(func(w *WriteUp) { w.ProposedBy = Actor{UserID: "x"} }), ErrNotOwner},
		{"agent", with(func(w *WriteUp) { w.ProposedBy = Actor{UserID: "lw", Agent: true} }), ErrAgentForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, g, err := CheckWriteUp(tt.w)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (!got.SeparateLine || !got.FlagForApprover || g != GateGB1) {
				t.Fatalf("got %+v gate %q, want separate flagged line at G-B1", got, g)
			}
		})
	}
}
