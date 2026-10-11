// Package timekeepingfake is an in-memory app/legal/timekeeping for tests: no network, no
// Postgres.
package timekeepingfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
)

var (
	_ tk.Entries      = (*Fake)(nil)
	_ tk.Timers       = (*Fake)(nil)
	_ tk.WriteDowns   = (*Fake)(nil)
	_ tk.ScopeChecker = (*Fake)(nil)
)

// FlagParalegalScope marks an entry outside the paralegal scope list.
const FlagParalegalScope = "paralegal_scope"

type edge struct{ from, to tk.State }

// transitions is the b3 TimeEntry state machine. The value says whether an agent may take it.
var transitions = map[edge]bool{
	{tk.StateSuggested, tk.StateAccepted}:     false,
	{tk.StateSuggested, tk.StateRejected}:     false,
	{tk.StateDraft, tk.StateAccepted}:         false,
	{tk.StateAccepted, tk.StateSubmitted}:     false,
	{tk.StateSubmitted, tk.StateApproved}:     false,
	{tk.StateAccepted, tk.StateApproved}:      false,
	{tk.StateSubmitted, tk.StateAccepted}:     false,
	{tk.StateApproved, tk.StateOnDraftBill}:   true,
	{tk.StateOnDraftBill, tk.StateApproved}:   false,
	{tk.StateOnDraftBill, tk.StateBilled}:     false,
	{tk.StateOnDraftBill, tk.StateWrittenOff}: false,
	{tk.StateApproved, tk.StateWrittenOff}:    false,
	{tk.StateBilled, tk.StateApproved}:        false,
}

// Fake keeps entries, timers and write-downs in memory. The tx argument is ignored.
type Fake struct {
	mu         sync.Mutex
	entries    map[tk.EntryID]tk.TimeEntry
	timers     map[tk.TimerID]tk.Timer
	writeDowns []tk.WriteDown
	paralegals map[string]bool
	scope      map[string]bool
	n          int
	// Now drives timers; tests replace it.
	Now func() time.Time
}

// New returns an empty fake. paralegals are user ids checked against scope (activity codes).
func New(paralegals, scope []string) *Fake {
	f := &Fake{entries: map[tk.EntryID]tk.TimeEntry{}, timers: map[tk.TimerID]tk.Timer{},
		paralegals: map[string]bool{}, scope: map[string]bool{}, Now: time.Now}
	for _, p := range paralegals {
		f.paralegals[p] = true
	}
	for _, s := range scope {
		f.scope[s] = true
	}
	return f
}

func (f *Fake) nextID(prefix string) string {
	f.n++
	return prefix + strconv.Itoa(f.n)
}

// CheckScope returns ErrOutOfScope for a paralegal outside the scope list.
func (f *Fake) CheckScope(_ context.Context, userID, activityCode string) error {
	if f.paralegals[userID] && !f.scope[activityCode] {
		return fmt.Errorf("scope %q: %w", activityCode, tk.ErrOutOfScope)
	}
	return nil
}

func (f *Fake) save(ctx context.Context, e tk.TimeEntry, state tk.State, origin tk.Origin) (tk.EntryID, error) {
	if e.MatterID == "" || e.UserID == "" || e.Minutes <= 0 {
		return "", fmt.Errorf("save entry: %w", tk.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e.ID = tk.EntryID(f.nextID("te-"))
	e.State, e.BillID, e.BilledMinutes = state, "", e.Minutes
	if e.Origin == "" {
		e.Origin = origin
	}
	e.Flags = nil
	if f.CheckScope(ctx, e.UserID, e.ActivityCode) != nil {
		e.Flags = []string{FlagParalegalScope}
	}
	f.entries[e.ID] = e
	return e.ID, nil
}

// Record saves a human entry as accepted.
func (f *Fake) Record(ctx context.Context, _ pgx.Tx, e tk.TimeEntry, by tk.Actor) (tk.EntryID, error) {
	if by.Agent {
		return "", fmt.Errorf("record: %w", tk.ErrAgentForbidden)
	}
	if by.UserID != e.UserID {
		return "", fmt.Errorf("record: %w", tk.ErrNotOwner)
	}
	return f.save(ctx, e, tk.StateAccepted, tk.OriginManual)
}

// Suggest saves an entry in state suggested.
func (f *Fake) Suggest(ctx context.Context, _ pgx.Tx, e tk.TimeEntry, _ tk.Actor) (tk.EntryID, error) {
	return f.save(ctx, e, tk.StateSuggested, tk.OriginSuggestion)
}

// Transition applies one state-machine edge.
func (f *Fake) Transition(_ context.Context, _ pgx.Tx, id tk.EntryID, to tk.State, by tk.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return fmt.Errorf("transition %s: %w", id, tk.ErrNotFound)
	}
	agentOK, ok := transitions[edge{e.State, to}]
	if !ok {
		return fmt.Errorf("transition %s %s->%s: %w", id, e.State, to, tk.ErrBadTransition)
	}
	switch {
	case by.Agent && !agentOK:
		return fmt.Errorf("transition %s->%s: %w", e.State, to, tk.ErrAgentForbidden)
	case e.State == tk.StateSuggested && to == tk.StateAccepted && by.UserID != e.UserID:
		return fmt.Errorf("accept %s: %w", id, tk.ErrNotOwner)
	case e.State == tk.StateSubmitted && to == tk.StateApproved && by.UserID == e.UserID:
		return fmt.Errorf("approve %s: %w", id, tk.ErrSelfApproval)
	case to == tk.StateOnDraftBill && len(e.Flags) > 0:
		return fmt.Errorf("bill %s: %w", id, tk.ErrOutOfScope)
	}
	// A release (on_draft_bill or billed back to approved) takes the entry off its bill,
	// so it can be billed again.
	if to == tk.StateApproved && (e.State == tk.StateOnDraftBill || e.State == tk.StateBilled) {
		e.BillID = ""
	}
	e.State = to
	f.entries[id] = e
	return nil
}

// Edit changes an entry that is not on a bill.
func (f *Fake) Edit(_ context.Context, _ pgx.Tx, e tk.TimeEntry, by tk.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.entries[e.ID]
	if !ok {
		return fmt.Errorf("edit %s: %w", e.ID, tk.ErrNotFound)
	}
	if by.Agent {
		return fmt.Errorf("edit %s: %w", e.ID, tk.ErrAgentForbidden)
	}
	if by.UserID != cur.UserID {
		return fmt.Errorf("edit %s: %w", e.ID, tk.ErrNotOwner)
	}
	if cur.BillID != "" || cur.State == tk.StateOnDraftBill || cur.State == tk.StateBilled {
		return fmt.Errorf("edit %s: %w", e.ID, tk.ErrBilledLocked)
	}
	if e.Minutes <= 0 {
		return fmt.Errorf("edit %s: %w", e.ID, tk.ErrInvalid)
	}
	cur.Minutes, cur.BilledMinutes, cur.Narrative, cur.ActivityCode = e.Minutes, e.Minutes, e.Narrative, e.ActivityCode
	f.entries[e.ID] = cur
	return nil
}

// Get returns one entry.
func (f *Fake) Get(_ context.Context, id tk.EntryID) (tk.TimeEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return tk.TimeEntry{}, fmt.Errorf("get %s: %w", id, tk.ErrNotFound)
	}
	return e, nil
}

// Start starts a timer; a user has at most one running.
func (f *Fake) Start(_ context.Context, _ pgx.Tx, userID, matterID string) (tk.TimerID, error) {
	if userID == "" || matterID == "" {
		return "", fmt.Errorf("start timer: %w", tk.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.timers {
		if t.UserID == userID && t.State == tk.TimerRunning {
			return "", fmt.Errorf("start timer: %w", tk.ErrTimerRunning)
		}
	}
	id := tk.TimerID(f.nextID("tm-"))
	f.timers[id] = tk.Timer{ID: id, UserID: userID, MatterID: matterID, StartedAt: f.Now(), State: tk.TimerRunning}
	return id, nil
}

// Stop stops a timer and returns raw whole minutes.
func (f *Fake) Stop(_ context.Context, _ pgx.Tx, id tk.TimerID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.timers[id]
	if !ok {
		return 0, fmt.Errorf("stop timer %s: %w", id, tk.ErrNotFound)
	}
	if t.State == tk.TimerStopped {
		return 0, fmt.Errorf("stop timer %s: %w", id, tk.ErrTimerStopped)
	}
	t.State = tk.TimerStopped
	f.timers[id] = t
	return int(f.Now().Sub(t.StartedAt)/time.Minute) - t.PausedMinutes, nil
}

// WriteDown records an adjustment; the entry's minutes never change.
func (f *Fake) WriteDown(_ context.Context, _ pgx.Tx, w tk.WriteDown) error {
	if w.ProposedBy.Agent {
		return fmt.Errorf("write-down: %w", tk.ErrAgentForbidden)
	}
	if w.AdjustedCents < 0 || w.OriginalCents < 0 || (w.Kind != "write_up" && w.AdjustedCents > w.OriginalCents) {
		return fmt.Errorf("write-down: %w", tk.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.entries[w.EntryID]; !ok {
		return fmt.Errorf("write-down %s: %w", w.EntryID, tk.ErrNotFound)
	}
	f.writeDowns = append(f.writeDowns, w)
	return nil
}

// WriteDownsFor returns the write-downs recorded against id.
func (f *Fake) WriteDownsFor(id tk.EntryID) []tk.WriteDown {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tk.WriteDown
	for _, w := range f.writeDowns {
		if w.EntryID == id {
			out = append(out, w)
		}
	}
	return out
}
