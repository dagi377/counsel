// Package deadlinefake is an in-memory kernel/deadline for tests: no network, no Postgres.
package deadlinefake

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/deadline"
)

var _ deadline.Engine = (*Fake)(nil)

// Fake keeps packs and deadlines in memory. The tx argument is ignored.
type Fake struct {
	mu        sync.Mutex
	packs     map[string]deadline.Pack
	deadlines map[deadline.DeadlineID]deadline.Deadline
	n         int
}

// New returns an empty engine.
func New() *Fake {
	return &Fake{packs: map[string]deadline.Pack{}, deadlines: map[deadline.DeadlineID]deadline.Deadline{}}
}

// LoadPack checks the pack's basic shape and stores it, replacing an older version.
func (f *Fake) LoadPack(_ context.Context, p deadline.Pack) error {
	if p.ID == "" || p.Version == "" {
		return fmt.Errorf("load pack: %w", deadline.ErrInvalidPack)
	}
	for _, r := range p.Rules {
		switch r.Mode {
		case deadline.Calendar, deadline.Clear, deadline.Business:
		default:
			return fmt.Errorf("pack %s rule %s mode %q: %w", p.ID, r.ID, r.Mode, deadline.ErrInvalidPack)
		}
		if r.ID == "" || r.Days < 0 || r.MonthsAfter < 0 {
			return fmt.Errorf("pack %s rule %q: %w", p.ID, r.ID, deadline.ErrInvalidPack)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.packs[p.ID] = p
	return nil
}

func toTime(d deadline.Date) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

func toDate(t time.Time) deadline.Date {
	return deadline.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

func businessDay(t time.Time, holidays map[deadline.Date]bool) bool {
	wd := t.Weekday()
	return wd != time.Saturday && wd != time.Sunday && !holidays[toDate(t)]
}

// Compute applies one rule: months first, then days by mode, then the roll rule.
func (f *Fake) Compute(_ context.Context, pack, rule string, trigger deadline.Date, subject deadline.Subject) ([]deadline.Proposed, error) {
	f.mu.Lock()
	p, ok := f.packs[pack]
	f.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("compute %s: %w", pack, deadline.ErrUnknownPack)
	}
	var r *deadline.Rule
	for i := range p.Rules {
		if p.Rules[i].ID == rule {
			r = &p.Rules[i]
		}
	}
	if r == nil {
		return nil, fmt.Errorf("compute %s/%s: %w", pack, rule, deadline.ErrUnknownRule)
	}
	hol := map[deadline.Date]bool{}
	for _, h := range p.Holidays {
		hol[h] = true
	}
	t := toTime(trigger).AddDate(0, r.MonthsAfter, 0)
	switch r.Mode {
	case deadline.Calendar:
		t = t.AddDate(0, 0, r.Days)
	case deadline.Clear:
		t = t.AddDate(0, 0, r.Days+1)
	case deadline.Business:
		for n := 0; n < r.Days; {
			t = t.AddDate(0, 0, 1)
			if businessDay(t, hol) {
				n++
			}
		}
	}
	if r.Roll == deadline.RollForward {
		for !businessDay(t, hol) {
			t = t.AddDate(0, 0, 1)
		}
	}
	return []deadline.Proposed{{PackID: p.ID, PackVersion: p.Version, RuleID: r.ID, Trigger: trigger, Due: toDate(t), Subject: subject}}, nil
}

// Save stores a proposal.
func (f *Fake) Save(_ context.Context, _ pgx.Tx, p deadline.Proposed) (deadline.DeadlineID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id := deadline.DeadlineID("dl-" + strconv.Itoa(f.n))
	f.deadlines[id] = deadline.Deadline{ID: id, Proposed: p, State: deadline.StateProposed}
	return id, nil
}

// Confirm records a person's confirmation; agents are refused.
func (f *Fake) Confirm(_ context.Context, _ pgx.Tx, id deadline.DeadlineID, by deadline.Principal) error {
	if by.Kind == "agent" || by.UserID == "" {
		return fmt.Errorf("confirm %s: %w", id, deadline.ErrAgentCannotConfirm)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.deadlines[id]
	if !ok {
		return fmt.Errorf("confirm %s: %w", id, deadline.ErrNotFound)
	}
	if d.State == deadline.StateConfirmed {
		return fmt.Errorf("confirm %s: %w", id, deadline.ErrAlreadyConfirmed)
	}
	d.State, d.ConfirmedBy = deadline.StateConfirmed, by.UserID
	f.deadlines[id] = d
	return nil
}

// Get returns one stored deadline.
func (f *Fake) Get(_ context.Context, id deadline.DeadlineID) (deadline.Deadline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.deadlines[id]
	if !ok {
		return deadline.Deadline{}, fmt.Errorf("get %s: %w", id, deadline.ErrNotFound)
	}
	return d, nil
}

var _ deadline.Lister = (*Fake)(nil)

// List returns the stored deadlines matching f, by due date then id.
func (f *Fake) List(_ context.Context, flt deadline.Filter) ([]deadline.Deadline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []deadline.Deadline
	for _, d := range f.deadlines {
		due := d.Proposed.Due
		if flt.Subject != nil && d.Proposed.Subject != *flt.Subject {
			continue
		}
		if flt.From != nil && due.Before(*flt.From) {
			continue
		}
		if flt.To != nil && flt.To.Before(due) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Proposed.Due, out[j].Proposed.Due
		if a != b {
			return a.Before(b)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
