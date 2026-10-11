package matter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/clock"
)

var (
	// ErrBadTransition is returned for a status change the lifecycle does not allow.
	ErrBadTransition = errors.New("matter: transition not allowed")
	// ErrNotCleared is returned when a matter would open without a G2 conflict clearance.
	ErrNotCleared = errors.New("matter: conflict check not cleared")
)

// transitions lists the allowed status changes. A closing matter may be reopened.
var transitions = map[Status][]Status{
	Pending: {Open},
	Open:    {Closing},
	Closing: {Open, Closed},
	Closed:  {Archived},
}

// CanTransition reports whether a matter may move from one status to another.
func CanTransition(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Lifecycle moves matters through their statuses. It reads conflict state only through the
// conflict module's API.
type Lifecycle struct {
	Clock     clock.Clock
	Conflicts conflict.Checker
	Walls     conflict.Walls
}

// Transition returns m moved to status to on behalf of actorID. A matter opens only when its
// latest conflict run is cleared through G2; opening sets OpenDate to today's business date.
// A walled actor gets ErrNotFound, never a hint that the matter exists.
func (l *Lifecycle) Transition(ctx context.Context, m Matter, actorID string, to Status) (Matter, error) {
	// rule: ARC-I-042
	walled, err := l.Walls.Walled(ctx, actorID, string(m.ID))
	if err != nil {
		return Matter{}, fmt.Errorf("matter: transition: %w", err)
	}
	if walled {
		return Matter{}, ErrNotFound
	}
	if !CanTransition(m.Status, to) {
		return Matter{}, fmt.Errorf("matter: %s to %s: %w", m.Status, to, ErrBadTransition)
	}
	if m.Status == Pending && to == Open {
		if err := l.requireCleared(ctx, m.ID); err != nil {
			return Matter{}, err
		}
		d := l.Clock.Today(ctx)
		m.OpenDate = time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, l.Clock.Location())
	}
	m.Status = to
	return m, nil
}

// requireCleared fails unless the matter's latest conflict run is cleared. A later open or
// rejected run supersedes an earlier clearance.
func (l *Lifecycle) requireCleared(ctx context.Context, id ID) error {
	// rule: ARC-I-041
	runs, err := l.Conflicts.Runs(ctx, string(id))
	if err != nil {
		return fmt.Errorf("matter: read conflict runs: %w", err)
	}
	if len(runs) == 0 || runs[len(runs)-1].Status != conflict.StatusCleared {
		return ErrNotCleared
	}
	return nil
}
