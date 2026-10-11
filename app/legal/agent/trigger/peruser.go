package trigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ErrNoUsers: a per-user binding has no source of active users, so its runs do not start.
var ErrNoUsers = errors.New("trigger: no active-user source for per-user feature")

// ReasonNoUserSource is the Unready reason for a per-user feature with no active-user source.
const ReasonNoUserSource = "no_user_source"

// ActiveUsers lists, per scheduled feature, the users it runs for on each tick (for
// F-TIM-01, every active timekeeper). It returns user ids only.
type ActiveUsers interface {
	ActiveUsers(ctx context.Context, featureID string) ([]string, error)
}

// BindPerUser builds d's binding like Bind, except that each scheduled slot fans out into one
// run per active user, each acting for that user only. Event runs still act for the event's
// user. A per-user feature needs a schedule.
func BindPerUser(d feature.Def, events ...string) (Binding, error) {
	if d.Source != feature.SourceJob {
		return Binding{}, fmt.Errorf("bind %s: source %q is not a job: %w", d.ID, d.Source, ErrBadBinding)
	}
	b := Binding{FeatureID: d.ID, Events: dedup(events), Cadence: ParseCadence(d.Trigger), PerUser: true}
	if b.Cadence == CadenceNone {
		return Binding{}, fmt.Errorf("bind %s: per-user run needs a schedule: %w", d.ID, ErrBadBinding)
	}
	if err := b.validate(); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// WithUsers sets where per-user bindings read their users. Without it every per-user
// binding is unready and never starts.
func (s *Bindings) WithUsers(u ActiveUsers) *Bindings {
	s.users = u
	return s
}

// usersFor lists the users a per-user binding's slot runs for, sorted and without repeats
// or empty ids. No source fails closed with ErrNoUsers.
func (s *Bindings) usersFor(ctx context.Context, b Binding) ([]string, error) {
	if s.users == nil {
		return nil, fmt.Errorf("feature %s: %w", b.FeatureID, ErrNoUsers)
	}
	got, err := s.users.ActiveUsers(ctx, b.FeatureID)
	if err != nil {
		return nil, fmt.Errorf("feature %s: active users: %w", b.FeatureID, err)
	}
	var out []string
	for _, u := range got {
		if u != "" && !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return out, nil
}

// tickPerUser enqueues one run of b in slot per active user. Each run acts for its own user
// and is unique by that user, so every user gets one run per slot and no run acts for two.
func (s *Bindings) tickPerUser(ctx context.Context, b Binding, slot string) (int, error) {
	users, err := s.usersFor(ctx, b)
	if errors.Is(err, ErrNoUsers) {
		slog.WarnContext(ctx, "scheduled agent feature not started: no active-user source", "feature", b.FeatureID, "slot", slot)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		// rule: LSO-3.6
		// A run acts for one user on that user's own time: ActingFor and ForUser are the
		// same id, never the rule admin's or another timekeeper's.
		args := RunArgs{FeatureID: b.FeatureID, ActingFor: u, ForUser: u, Slot: slot}
		if err := s.insert(ctx, args); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
