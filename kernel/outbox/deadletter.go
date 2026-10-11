package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/registry"
)

// Dead letter errors. Callers match them with errors.Is.
var (
	// ErrNotAdmin means a replay was asked for by a principal that is not an admin user.
	// Agents and guests never replay.
	ErrNotAdmin = errors.New("outbox: dead letter replay needs an admin user")
	// ErrNoDeadLetters means a replay named no dead letter ids.
	ErrNoDeadLetters = errors.New("outbox: replay names no dead letters")
)

// Audit actions written by Replay.
const (
	ActionReplayRequested = "outbox.deadletter.replay_requested"
	ActionReplayDone      = "outbox.deadletter.replay_done"
)

// DeadLetter is a message that ran out of retries. LastError is already redacted.
type DeadLetter struct {
	ID         string
	Message    Message
	DeadAt     time.Time
	LastError  string
	ReplayedAt *time.Time
}

// DeadLetterStore reads dead letters and puts them back on the outbox.
type DeadLetterStore interface {
	// Open returns dead letters not yet replayed, oldest first.
	Open(ctx context.Context) ([]DeadLetter, error)
	// Requeue makes the dead letter's message pending again and stamps ReplayedAt. It
	// returns false, and changes nothing, when the dead letter was already replayed.
	// An unknown id returns ErrNotFound.
	Requeue(ctx context.Context, id string, at time.Time) (requeued bool, err error)
}

// Pager raises a page. key is stable per dead letter so the pager can dedupe.
type Pager interface {
	Page(ctx context.Context, key, summary string) error
}

// ReplayAuditor records replay actions. detail never holds payload or error text.
type ReplayAuditor interface {
	Record(ctx context.Context, by registry.Principal, action, deadLetterID string, detail map[string]string) error
}

// DeadLetters pages on untriaged dead letters and replays them on an admin's request.
type DeadLetters struct {
	Store   DeadLetterStore
	Pager   Pager
	Audit   ReplayAuditor
	IsAdmin func(ctx context.Context, p registry.Principal) bool
	// Holiday reports non-business dates in Loc; nil means weekends only.
	Holiday func(d time.Time) bool
	Loc     *time.Location
	Now     func() time.Time
}

func (d *DeadLetters) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *DeadLetters) loc() *time.Location {
	if d.Loc != nil {
		return d.Loc
	}
	return time.UTC
}

func (d *DeadLetters) businessDay(t time.Time) bool {
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return d.Holiday == nil || !d.Holiday(t)
}

// TriageDue returns when a dead letter that died at deadAt is overdue: one business day
// later, at the same clock time. A dead letter that died outside a business day counts
// from the start of the next business day.
func (d *DeadLetters) TriageDue(deadAt time.Time) time.Time {
	t := deadAt.In(d.loc())
	if !d.businessDay(t) {
		y, m, dd := t.Date()
		t = time.Date(y, m, dd, 0, 0, 0, 0, t.Location())
		for !d.businessDay(t) {
			t = t.AddDate(0, 0, 1)
		}
	}
	t = t.AddDate(0, 0, 1)
	for !d.businessDay(t) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// PageOverdue pages once per open dead letter older than one business day and returns
// the ids it paged. A page failure stops the sweep.
func (d *DeadLetters) PageOverdue(ctx context.Context) ([]string, error) {
	open, err := d.Store.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("page dead letters: %w", err)
	}
	now := d.now()
	var paged []string
	for _, dl := range open {
		// rule: ARC-U-024
		if now.Before(d.TriageDue(dl.DeadAt)) {
			continue
		}
		summary := fmt.Sprintf("dead letter %s (%s) untriaged since %s", dl.ID, dl.Message.Type, dl.DeadAt.In(d.loc()).Format(time.RFC3339))
		if err := d.Pager.Page(ctx, "outbox.deadletter:"+dl.ID, summary); err != nil {
			return paged, fmt.Errorf("page dead letter %s: %w", dl.ID, err)
		}
		paged = append(paged, dl.ID)
	}
	return paged, nil
}

// Replay requeues each named dead letter (one message or a batch). Only an admin user may
// replay. Each id gets a replay_requested audit row, then a replay_done row whose
// "requeued" detail says whether this call put it back; a second replay of the same id
// changes nothing, so the effect happens once.
func (d *DeadLetters) Replay(ctx context.Context, by registry.Principal, ids []string) (requeued []string, err error) {
	// rule: ARC-U-024
	if by.Kind != registry.PrincipalUser || by.UserID == "" || d.IsAdmin == nil || !d.IsAdmin(ctx, by) {
		return nil, fmt.Errorf("replay dead letters: %w", ErrNotAdmin)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("replay dead letters: %w", ErrNoDeadLetters)
	}
	for _, id := range ids {
		if err := d.Audit.Record(ctx, by, ActionReplayRequested, id, nil); err != nil {
			return requeued, fmt.Errorf("replay %s: audit request: %w", id, err)
		}
		ok, err := d.Store.Requeue(ctx, id, d.now())
		if err != nil {
			return requeued, fmt.Errorf("replay %s: %w", id, err)
		}
		if err := d.Audit.Record(ctx, by, ActionReplayDone, id, map[string]string{"requeued": fmt.Sprint(ok)}); err != nil {
			return requeued, fmt.Errorf("replay %s: audit done: %w", id, err)
		}
		if ok {
			requeued = append(requeued, id)
		}
	}
	return requeued, nil
}
