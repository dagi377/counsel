package deadline

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/dagi377/counsel/kernel/registry"
)

// Confirmation is one entry in the append-only confirmation log: a person agreed that a
// deadline is due on Due. Entries are never changed or removed.
type Confirmation struct {
	DeadlineID DeadlineID
	UserID     string
	Due        Date
}

// Book keeps stored deadlines and their append-only confirmation log. Every computed
// deadline starts as proposed and only a person moves it to confirmed. It is safe for
// concurrent use.
type Book struct {
	mu        sync.Mutex
	deadlines map[DeadlineID]Deadline
	log       []Confirmation
	n         int
}

// NewBook returns an empty book.
func NewBook() *Book {
	return &Book{deadlines: map[DeadlineID]Deadline{}}
}

// Save stores p in state proposed and returns its id.
func (b *Book) Save(_ context.Context, p Proposed) (DeadlineID, error) {
	if p.PackID == "" || p.RuleID == "" {
		return "", fmt.Errorf("save deadline: missing pack or rule: %w", ErrInvalidPack)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n++
	id := DeadlineID("deadline-" + strconv.Itoa(b.n))
	b.deadlines[id] = Deadline{ID: id, Proposed: p, State: StateProposed}
	return id, nil
}

// Confirm records by's confirmation of deadline id. Only a person (a user principal with a
// user id) confirms; an agent, a guest or an anonymous principal is refused.
func (b *Book) Confirm(_ context.Context, id DeadlineID, by Principal) error {
	// Fail closed: anything that is not a named user is refused.
	if by.Kind != registry.PrincipalUser || by.UserID == "" {
		return fmt.Errorf("confirm %s: %w", id, ErrAgentCannotConfirm)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.deadlines[id]
	if !ok {
		return fmt.Errorf("confirm %s: %w", id, ErrNotFound)
	}
	if d.State == StateConfirmed {
		return fmt.Errorf("confirm %s: %w", id, ErrAlreadyConfirmed)
	}
	d.State, d.ConfirmedBy = StateConfirmed, by.UserID
	b.deadlines[id] = d
	b.log = append(b.log, Confirmation{DeadlineID: id, UserID: by.UserID, Due: d.Proposed.Due})
	return nil
}

// Get returns one stored deadline.
func (b *Book) Get(_ context.Context, id DeadlineID) (Deadline, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.deadlines[id]
	if !ok {
		return Deadline{}, fmt.Errorf("get %s: %w", id, ErrNotFound)
	}
	return d, nil
}

// Confirmations returns a copy of the confirmation log entries for id, oldest first.
func (b *Book) Confirmations(_ context.Context, id DeadlineID) []Confirmation {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Confirmation
	for _, c := range b.log {
		if c.DeadlineID == id {
			out = append(out, c)
		}
	}
	return out
}

// Repack applies a new version of pack p to the stored deadlines of that pack. Unconfirmed
// deadlines are re-computed from p and take its version; confirmed deadlines are never
// touched. It returns the ids it re-computed, sorted. If any unconfirmed deadline cannot be
// re-computed (for example its rule is gone), nothing changes and the error is returned.
func (b *Book) Repack(_ context.Context, p Pack) ([]DeadlineID, error) {
	if p.ID == "" || p.Version == "" {
		return nil, fmt.Errorf("repack: %w", ErrInvalidPack)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	next := map[DeadlineID]Deadline{}
	for id, d := range b.deadlines {
		if d.Proposed.PackID != p.ID || d.State == StateConfirmed || d.Proposed.PackVersion == p.Version {
			continue
		}
		due, err := Due(p, d.Proposed.RuleID, d.Proposed.Trigger)
		if err != nil {
			return nil, fmt.Errorf("repack %s: %w", id, err)
		}
		d.Proposed.PackVersion, d.Proposed.Due = p.Version, due
		next[id] = d
	}
	ids := make([]DeadlineID, 0, len(next))
	for id, d := range next {
		b.deadlines[id] = d
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
