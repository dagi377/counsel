package agenttools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// PlanStore persists agent plans. A plan and its steps, with each step's linked tool calls,
// are saved as one unit after every change, so a run that crashes resumes from the store.
// Load methods return ErrNotFound when nothing is stored.
type PlanStore interface {
	ByRun(ctx context.Context, runID string) (Plan, error)
	ByID(ctx context.Context, planID string) (Plan, error)
	Save(ctx context.Context, p Plan) error
}

var _ Plans = (*Planner)(nil)

// Planner implements Plans over a PlanStore. It keeps no plan state of its own: every read
// goes to the store, so a new Planner over the same store picks up where a crashed one left.
type Planner struct {
	store PlanStore
	newID func() (string, error)
	mu    sync.Mutex // serialises read-modify-write on the store within one process
}

// NewPlanner returns a Planner that persists to store.
func NewPlanner(store PlanStore) *Planner {
	return &Planner{store: store, newID: randomID}
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("plan id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ReportPlan implements Plans. Reporting the same step titles again for a run (an agent that
// restarted after a crash) returns the stored plan with its progress; different titles store
// a new plan for the run with every step pending.
func (pl *Planner) ReportPlan(ctx context.Context, runID string, titles []string) (Plan, error) {
	if runID == "" {
		return Plan{}, fmt.Errorf("report plan: run id required: %w", ErrNotFound)
	}
	if len(titles) == 0 {
		return Plan{}, fmt.Errorf("report plan: no steps: %w", ErrStepTitle)
	}
	for i, t := range titles {
		// rule: ARC-I-039
		if strings.TrimSpace(t) == "" || len([]rune(t)) > MaxStepTitle {
			return Plan{}, fmt.Errorf("report plan: step %d: %w", i, ErrStepTitle)
		}
	}
	pl.mu.Lock()
	defer pl.mu.Unlock()
	old, err := pl.store.ByRun(ctx, runID)
	switch {
	case err == nil && sameTitles(old, titles):
		return old, nil
	case err != nil && !errors.Is(err, ErrNotFound):
		return Plan{}, fmt.Errorf("report plan %s: %w", runID, err)
	}
	id, err := pl.newID()
	if err != nil {
		return Plan{}, fmt.Errorf("report plan %s: %w", runID, err)
	}
	p := Plan{ID: id, RunID: runID, Steps: make([]Step, len(titles))}
	for i, t := range titles {
		p.Steps[i] = Step{Index: i, Title: t, Status: StepPending}
	}
	if err := pl.store.Save(ctx, p); err != nil {
		return Plan{}, fmt.Errorf("report plan %s: %w", runID, err)
	}
	return p, nil
}

func sameTitles(p Plan, titles []string) bool {
	if len(p.Steps) != len(titles) {
		return false
	}
	for i, s := range p.Steps {
		if s.Title != titles[i] {
			return false
		}
	}
	return true
}

// ReportStep implements Plans.
func (pl *Planner) ReportStep(ctx context.Context, planID string, index int, status StepStatus) error {
	switch status {
	case StepPending, StepRunning, StepDone, StepFailed:
	default:
		return fmt.Errorf("report step %s/%d: status %q: %w", planID, index, status, ErrStepTitle)
	}
	pl.mu.Lock()
	defer pl.mu.Unlock()
	p, err := pl.store.ByID(ctx, planID)
	if err != nil {
		return fmt.Errorf("report step %s/%d: %w", planID, index, err)
	}
	if index < 0 || index >= len(p.Steps) {
		return fmt.Errorf("report step %s/%d: %w", planID, index, ErrNotFound)
	}
	p.Steps[index].Status = status
	if err := pl.store.Save(ctx, p); err != nil {
		return fmt.Errorf("report step %s/%d: %w", planID, index, err)
	}
	return nil
}

// Current implements Plans. The first unfinished step is the first step not done; a failed
// step counts as unfinished, so a resumed run retries it.
func (pl *Planner) Current(ctx context.Context, runID string) (Plan, int, error) {
	p, err := pl.store.ByRun(ctx, runID)
	if err != nil {
		return Plan{}, -1, fmt.Errorf("current plan %s: %w", runID, err)
	}
	return p, firstUnfinished(p), nil
}

// RecordCall links a tool call to the run's first unfinished step and marks that step running
// if it was pending. It returns the step index, or ErrNotFound when the run has no plan or
// every step is done.
func (pl *Planner) RecordCall(ctx context.Context, runID, callID string) (int, error) {
	if callID == "" {
		return -1, fmt.Errorf("record call %s: call id required: %w", runID, ErrNotFound)
	}
	pl.mu.Lock()
	defer pl.mu.Unlock()
	p, err := pl.store.ByRun(ctx, runID)
	if err != nil {
		return -1, fmt.Errorf("record call %s: %w", runID, err)
	}
	i := firstUnfinished(p)
	if i < 0 {
		return -1, fmt.Errorf("record call %s: no unfinished step: %w", runID, ErrNotFound)
	}
	s := &p.Steps[i]
	if !slices.Contains(s.CallIDs, callID) {
		s.CallIDs = append(s.CallIDs, callID)
	}
	if s.Status == StepPending {
		s.Status = StepRunning
	}
	if err := pl.store.Save(ctx, p); err != nil {
		return -1, fmt.Errorf("record call %s: %w", runID, err)
	}
	return i, nil
}

func firstUnfinished(p Plan) int {
	for i, s := range p.Steps {
		if s.Status != StepDone {
			return i
		}
	}
	return -1
}
