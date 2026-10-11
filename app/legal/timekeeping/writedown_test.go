package timekeeping

import (
	"errors"
	"testing"
)

// rule: BIL-C-013
func TestBIL_C_013_WriteDownChangesLineNotMinutes(t *testing.T) {
	lawyer := Actor{UserID: "u-resp"}
	approved := TimeEntry{ID: "te-1", UserID: "u-tk", Minutes: 204, BilledMinutes: 204, State: StateApproved}
	onDraft := approved
	onDraft.State, onDraft.BillID = StateOnDraftBill, "bill-1"
	billed := approved
	billed.State, billed.BillID = StateBilled, "bill-1"
	wd := func(kind, stage string, orig, adj int64, by Actor) WriteDown {
		return WriteDown{EntryID: "te-1", OriginalCents: orig, AdjustedCents: adj, Kind: kind,
			ReasonCode: "courtesy", Stage: stage, ProposedBy: by}
	}
	tests := []struct {
		name      string
		entry     TimeEntry
		w         WriteDown
		wantLine  int64
		wantGate  Gate
		wantOwner bool
		wantErr   error
	}{
		{"spec test 1445.00 to 1200.00, minutes stay 204", approved, wd(KindWriteDown, StagePreBill, 144500, 120000, lawyer), 120000, GateG9d, false, nil},
		{"on draft bill uses G-B1", onDraft, wd(KindWriteDown, StagePreBill, 144500, 120000, lawyer), 120000, GateGB1, false, nil},
		{"above threshold needs owner role", approved, wd(KindWriteDown, StagePreBill, 144500, 50000, lawyer), 50000, GateG9d, true, nil},
		{"exactly at threshold does not", approved, wd(KindWriteDown, StagePreBill, 100000, 50000, lawyer), 50000, GateG9d, false, nil},
		{"post-bill on billed entry", billed, wd(KindWriteDown, StagePostBill, 144500, 140000, lawyer), 140000, GateG9d, false, nil},
		{"write-up raises line", approved, wd(KindWriteUp, StagePreBill, 100000, 110000, lawyer), 110000, GateG9d, false, nil},
		{"agent cannot write down", approved, wd(KindWriteDown, StagePreBill, 144500, 120000, Actor{UserID: "u-resp", Agent: true}), 0, GateNone, false, ErrAgentForbidden},
		{"write-down cannot raise", approved, wd(KindWriteDown, StagePreBill, 100000, 120000, lawyer), 0, GateNone, false, ErrInvalid},
		{"negative amount", approved, wd(KindWriteDown, StagePreBill, 100000, -1, lawyer), 0, GateNone, false, ErrInvalid},
		{"unknown kind", approved, wd("discount", StagePreBill, 100000, 90000, lawyer), 0, GateNone, false, ErrInvalid},
		{"unknown stage", approved, wd(KindWriteDown, "later", 100000, 90000, lawyer), 0, GateNone, false, ErrInvalid},
		{"pre-bill on billed entry", billed, wd(KindWriteDown, StagePreBill, 100000, 90000, lawyer), 0, GateNone, false, ErrBadTransition},
		{"post-bill on unbilled entry", approved, wd(KindWriteDown, StagePostBill, 100000, 90000, lawyer), 0, GateNone, false, ErrBadTransition},
		{"reason required", approved, WriteDown{EntryID: "te-1", OriginalCents: 100, AdjustedCents: 50, Kind: KindWriteDown, Stage: StagePreBill, ProposedBy: lawyer}, 0, GateNone, false, ErrInvalid},
		{"other entry id", approved, WriteDown{EntryID: "te-2", OriginalCents: 100, AdjustedCents: 50, Kind: KindWriteDown, ReasonCode: "x", Stage: StagePreBill, ProposedBy: lawyer}, 0, GateNone, false, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckWriteDown(tt.entry, tt.w)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.LineAmountCents != tt.wantLine || got.Gate != tt.wantGate || got.NeedsOwnerRole != tt.wantOwner {
				t.Fatalf("got line %d gate %q owner %v, want %d %q %v", got.LineAmountCents, got.Gate, got.NeedsOwnerRole, tt.wantLine, tt.wantGate, tt.wantOwner)
			}
			if got.Entry.Minutes != tt.entry.Minutes || got.Entry.BilledMinutes != tt.entry.BilledMinutes {
				t.Fatalf("minutes changed: %d/%d, want %d/%d", got.Entry.Minutes, got.Entry.BilledMinutes, tt.entry.Minutes, tt.entry.BilledMinutes)
			}
		})
	}
}

// rule: BIL-C-013
func TestBIL_C_013_BilledEntryLocked(t *testing.T) {
	tests := []struct {
		name    string
		e       TimeEntry
		wantErr error
	}{
		{"approved, no bill", TimeEntry{ID: "te-1", State: StateApproved}, nil},
		{"accepted, no bill", TimeEntry{ID: "te-1", State: StateAccepted}, nil},
		{"on draft bill -> 409", TimeEntry{ID: "te-1", State: StateOnDraftBill, BillID: "b1"}, ErrBilledLocked},
		{"billed -> 409", TimeEntry{ID: "te-1", State: StateBilled, BillID: "b1"}, ErrBilledLocked},
		{"carries a bill id -> 409", TimeEntry{ID: "te-1", State: StateApproved, BillID: "b1"}, ErrBilledLocked},
		{"bill voided, back to approved", TimeEntry{ID: "te-1", State: StateApproved}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckEditable(tt.e); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
