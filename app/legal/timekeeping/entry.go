package timekeeping

import "fmt"

// Gate names the approval gate a TimeEntry transition needs ("" when none).
type Gate string

// Gates used by the TimeEntry state machine.
const (
	GateNone Gate = ""
	GateG9   Gate = "G9"
	GateG9c  Gate = "G9c"
	GateG9d  Gate = "G9d"
	GateGB1  Gate = "G-B1"
	GateGB2  Gate = "G-B2"
	actorSys      = "system"
)

type transition struct{ from, to State }

// edgeRule says who may take one TimeEntry transition and which gate it needs.
type edgeRule struct {
	gate       Gate
	agentMay   bool // an agent may take it (draft build only)
	ownerOnly  bool // only the entry owner may take it
	notSelf    bool // the entry owner may not take it (approval)
	systemOnly bool // taken only by the system (Actor with empty UserID, not an agent)
}

// stateMachine is the b3 TimeEntry state machine (spec/eng/b3-billing-engine.json).
var stateMachine = map[transition]edgeRule{
	{StateSuggested, StateAccepted}:     {gate: GateG9, ownerOnly: true},
	{StateSuggested, StateRejected}:     {},
	{StateDraft, StateAccepted}:         {},
	{StateAccepted, StateSubmitted}:     {},
	{StateSubmitted, StateApproved}:     {gate: GateG9c, notSelf: true},
	{StateAccepted, StateApproved}:      {systemOnly: true},
	{StateSubmitted, StateAccepted}:     {},
	{StateApproved, StateOnDraftBill}:   {agentMay: true},
	{StateOnDraftBill, StateApproved}:   {gate: GateGB1},
	{StateOnDraftBill, StateBilled}:     {gate: GateGB2},
	{StateOnDraftBill, StateWrittenOff}: {gate: GateG9d},
	{StateApproved, StateWrittenOff}:    {gate: GateG9d},
	{StateBilled, StateApproved}:        {gate: GateGB2},
}

// TransitionGate returns the gate the from -> to transition needs, or ErrBadTransition.
func TransitionGate(from, to State) (Gate, error) {
	r, ok := stateMachine[transition{from, to}]
	if !ok {
		return GateNone, fmt.Errorf("%s -> %s: %w", from, to, ErrBadTransition)
	}
	return r.gate, nil
}

// CheckTransition says whether by may move e to state to. It checks the state machine and
// the actor rules; the caller still opens the returned gate.
func CheckTransition(e TimeEntry, to State, by Actor) (Gate, error) {
	r, ok := stateMachine[transition{e.State, to}]
	if !ok {
		return GateNone, fmt.Errorf("entry %s %s -> %s: %w", e.ID, e.State, to, ErrBadTransition)
	}
	// An agent only suggests; a suggestion is never auto-accepted (G9 by the owner).
	if by.Agent && !r.agentMay {
		return GateNone, fmt.Errorf("entry %s %s -> %s: %w", e.ID, e.State, to, ErrAgentForbidden)
	}
	if r.systemOnly && by.UserID != "" {
		return GateNone, fmt.Errorf("entry %s %s -> %s by %s: only %s: %w", e.ID, e.State, to, by.UserID, actorSys, ErrBadTransition)
	}
	if r.ownerOnly && by.UserID != e.UserID {
		return GateNone, fmt.Errorf("entry %s accept by %s: %w", e.ID, by.UserID, ErrNotOwner)
	}
	if r.notSelf && by.UserID == e.UserID {
		return GateNone, fmt.Errorf("entry %s approve by %s: %w", e.ID, by.UserID, ErrSelfApproval)
	}
	return r.gate, nil
}

// CheckAttach says whether e may go on bill billID.
// rule: BIL-I-007
// An entry is on at most one non-void bill: an entry already carrying another BillID is
// skipped by the second draft build.
func CheckAttach(e TimeEntry, billID string) error {
	if billID == "" {
		return fmt.Errorf("entry %s: bill id required: %w", e.ID, ErrInvalid)
	}
	if e.BillID != "" && e.BillID != billID {
		return fmt.Errorf("entry %s already on bill %s: %w", e.ID, e.BillID, ErrBilledLocked)
	}
	if e.State != StateApproved && !(e.State == StateOnDraftBill && e.BillID == billID) {
		return fmt.Errorf("entry %s in %s: %w", e.ID, e.State, ErrBadTransition)
	}
	return nil
}

// Line kinds a write-up may not be added to (BIL-C-014).
const (
	LineContingency = "contingency"
	LineFlat        = "flat"
)

// WriteUp is a value-adjustment bill line proposed at G-B1.
type WriteUp struct {
	EntryID          EntryID
	AmountCents      int64  // the adjustment, > 0
	ReasonCode       string // required
	LineKind         string // kind of the line it adjusts ("hourly", "flat", "contingency" ...)
	ProposedBy       Actor
	ResponsibleUser  string // the matter's responsible lawyer
	FlagForApprover  bool   // set by CheckWriteUp: the G-B2 approver sees it
	SeparateLine     bool   // set by CheckWriteUp: always its own bill line
	AdjustsMinutes   bool   // must stay false
	AdjustsRateCents bool   // must stay false
}

// CheckWriteUp validates a write-up and returns it marked as a separate, flagged line.
// rule: BIL-C-014
// A write-up is a separate value-adjustment line with a reason code, added by the
// responsible lawyer at G-B1, flagged at G-B2; never raises minutes or rate; never on
// contingency or flat lines.
func CheckWriteUp(w WriteUp) (WriteUp, Gate, error) {
	switch {
	case w.ProposedBy.Agent:
		return w, GateNone, fmt.Errorf("write-up %s: %w", w.EntryID, ErrAgentForbidden)
	case w.AmountCents <= 0:
		return w, GateNone, fmt.Errorf("write-up %s: amount must be positive: %w", w.EntryID, ErrInvalid)
	case w.ReasonCode == "":
		return w, GateNone, fmt.Errorf("write-up %s: reason code required: %w", w.EntryID, ErrInvalid)
	case w.LineKind == LineContingency || w.LineKind == LineFlat:
		return w, GateNone, fmt.Errorf("write-up %s on %s line: %w", w.EntryID, w.LineKind, ErrInvalid)
	case w.AdjustsMinutes || w.AdjustsRateCents:
		return w, GateNone, fmt.Errorf("write-up %s: never changes minutes or rate: %w", w.EntryID, ErrInvalid)
	case w.ResponsibleUser == "" || w.ProposedBy.UserID != w.ResponsibleUser:
		return w, GateNone, fmt.Errorf("write-up %s by %s: %w", w.EntryID, w.ProposedBy.UserID, ErrNotOwner)
	}
	w.SeparateLine = true
	w.FlagForApprover = true
	return w, GateGB1, nil
}
