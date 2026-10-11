package identity

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// BreakGlassMax is the longest a break-glass grant may last (audit.break_glass CHECK).
const BreakGlassMax = 8 * time.Hour

// Audit actions written by break-glass.
const (
	ActionBreakGlassOpen   = "core.break_glass_open"
	ActionBreakGlassExpire = "core.break_glass_expire"
)

// ErrInvalidBreakGlass means a break-glass request is missing a field, is not made by a
// user, or asks for a duration outside (0, BreakGlassMax].
var ErrInvalidBreakGlass = errors.New("identity: invalid break-glass request")

// BreakGlassRequest asks for emergency access to one subject the requester cannot see.
// Reason and Explanation are stored with the grant, never in audit detail or logs.
type BreakGlassRequest struct {
	By          Principal
	Subject     Subject
	Reason      string
	Explanation string
	Duration    time.Duration
}

// BreakGlassGrant is one time-boxed emergency grant.
type BreakGlassGrant struct {
	ID          string
	UserID      UserID
	Subject     Subject
	Reason      string
	Explanation string
	StartsAt    time.Time
	EndsAt      time.Time
	Expired     bool // set once the expiry audit event is written
}

// ActiveAt reports whether the grant is in force at t. A grant ends at EndsAt whether or
// not Expire has run yet.
func (g BreakGlassGrant) ActiveAt(t time.Time) bool {
	return !t.Before(g.StartsAt) && t.Before(g.EndsAt)
}

// BreakGlass opens and expires time-boxed emergency grants. Every open and every expiry
// writes one audit event in the caller's transaction.
type BreakGlass struct {
	clock clock.Clock
	log   audit.Log

	mu     sync.Mutex
	seq    int
	grants []*BreakGlassGrant
}

// NewBreakGlass returns a BreakGlass that reads time from c and audits to log.
func NewBreakGlass(c clock.Clock, log audit.Log) *BreakGlass {
	return &BreakGlass{clock: c, log: log}
}

// Open validates r, records a grant from now to now+r.Duration and appends an audit event.
// Only a user principal may break glass; an agent never does.
func (b *BreakGlass) Open(ctx context.Context, tx pgx.Tx, r BreakGlassRequest) (BreakGlassGrant, error) {
	switch {
	case r.By.Kind != registry.PrincipalUser || r.By.UserID == "":
		return BreakGlassGrant{}, fmt.Errorf("break glass: principal %q: %w", r.By.Kind, ErrInvalidBreakGlass)
	case r.Subject.Kind == "" || r.Subject.ID == "":
		return BreakGlassGrant{}, fmt.Errorf("break glass: no subject: %w", ErrInvalidBreakGlass)
	case strings.TrimSpace(r.Reason) == "" || strings.TrimSpace(r.Explanation) == "":
		return BreakGlassGrant{}, fmt.Errorf("break glass: reason and explanation required: %w", ErrInvalidBreakGlass)
	case r.Duration <= 0 || r.Duration > BreakGlassMax:
		return BreakGlassGrant{}, fmt.Errorf("break glass: duration %s: %w", r.Duration, ErrInvalidBreakGlass)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	b.seq++
	g := &BreakGlassGrant{
		ID:          "bg-" + strconv.Itoa(b.seq),
		UserID:      UserID(r.By.UserID),
		Subject:     r.Subject,
		Reason:      r.Reason,
		Explanation: r.Explanation,
		StartsAt:    now,
		EndsAt:      now.Add(r.Duration),
	}
	if err := b.log.Append(ctx, tx, b.event(ActionBreakGlassOpen, *g)); err != nil {
		b.seq--
		return BreakGlassGrant{}, fmt.Errorf("break glass: audit open: %w", err)
	}
	b.grants = append(b.grants, g)
	return *g, nil
}

// Active returns the grant that lets user see s now, if any.
func (b *BreakGlass) Active(_ context.Context, user UserID, s Subject) (BreakGlassGrant, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	for _, g := range b.grants {
		if g.UserID == user && g.Subject == s && g.ActiveAt(now) {
			return *g, true
		}
	}
	return BreakGlassGrant{}, false
}

// Expire marks every grant past its EndsAt as expired and appends one expiry audit event
// per grant. It is safe to run repeatedly; it returns how many grants it expired.
func (b *BreakGlass) Expire(ctx context.Context, tx pgx.Tx) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock.Now()
	n := 0
	for _, g := range b.grants {
		if g.Expired || now.Before(g.EndsAt) {
			continue
		}
		if err := b.log.Append(ctx, tx, b.event(ActionBreakGlassExpire, *g)); err != nil {
			return n, fmt.Errorf("break glass: audit expire %s: %w", g.ID, err)
		}
		g.Expired = true
		n++
	}
	return n, nil
}

// Grants lists every grant, in the order opened, for break-glass review.
func (b *BreakGlass) Grants(_ context.Context) []BreakGlassGrant {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BreakGlassGrant, len(b.grants))
	for i, g := range b.grants {
		out[i] = *g
	}
	return out
}

// event builds the audit event for g. Reason and explanation are free text and stay out
// of audit detail.
func (b *BreakGlass) event(action string, g BreakGlassGrant) audit.Event {
	e := audit.Event{
		ActorKind: string(registry.PrincipalUser),
		ActorID:   string(g.UserID),
		Action:    action,
		Subject:   g.Subject,
		Detail: map[string]string{
			"grant_id":  g.ID,
			"starts_at": g.StartsAt.UTC().Format(time.RFC3339),
			"ends_at":   g.EndsAt.UTC().Format(time.RFC3339),
		},
	}
	if action == ActionBreakGlassExpire {
		e.ActorKind, e.ActorID = "system", "break_glass_expiry"
		e.Detail["user_id"] = string(g.UserID)
	}
	return e
}
