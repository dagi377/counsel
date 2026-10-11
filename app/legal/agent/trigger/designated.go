package trigger

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// ErrNoDesignatedLawyer: a scheduled feature has no designated lawyer in firm settings, so
// its run does not start.
var ErrNoDesignatedLawyer = errors.New("trigger: no designated lawyer for scheduled feature")

// DesignatedLawyers reads the firm setting that names, per scheduled feature, the lawyer its
// runs act for (agent.designated_lawyer). DesignatedLawyer returns "" with a nil error when
// the firm has named nobody for the feature.
type DesignatedLawyers interface {
	DesignatedLawyer(ctx context.Context, featureID string) (string, error)
}

// BindDesignated builds d's binding like Bind, except that a scheduled run acts for the
// feature's designated lawyer, read from firm settings at each tick, so a change of lawyer
// needs no restart. Event runs still act for the event's user.
func BindDesignated(d feature.Def, events ...string) (Binding, error) {
	if d.Source != feature.SourceJob {
		return Binding{}, fmt.Errorf("bind %s: source %q is not a job: %w", d.ID, d.Source, ErrBadBinding)
	}
	b := Binding{FeatureID: d.ID, Events: dedup(events), Cadence: ParseCadence(d.Trigger), Designated: true}
	if b.Cadence == CadenceNone {
		return Binding{}, fmt.Errorf("bind %s: designated lawyer needs a schedule: %w", d.ID, ErrBadBinding)
	}
	if err := b.validate(); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// WithDesignated sets where designated bindings read their lawyer. Without it every
// designated binding is unready and never starts.
func (s *Bindings) WithDesignated(l DesignatedLawyers) *Bindings {
	s.lawyers = l
	return s
}

// Unready is one scheduled feature that cannot start, by id, with a machine reason.
type Unready struct {
	FeatureID string `json:"feature_id"`
	Reason    string `json:"reason"`
}

// ReasonNoDesignatedLawyer is the Unready reason for a scheduled feature with no designated
// lawyer in firm settings.
const ReasonNoDesignatedLawyer = "no_designated_lawyer"

// Readiness reports every designated binding whose feature has no designated lawyer, and
// every per-user binding with no active-user source, in the order they were added. An empty result means every scheduled feature can start.
func (s *Bindings) Readiness(ctx context.Context) ([]Unready, error) {
	var out []Unready
	for _, b := range s.sched {
		if b.PerUser && s.users == nil {
			out = append(out, Unready{FeatureID: b.FeatureID, Reason: ReasonNoUserSource})
			continue
		}
		if !b.Designated {
			continue
		}
		_, err := s.actingFor(ctx, b)
		if errors.Is(err, ErrNoDesignatedLawyer) {
			out = append(out, Unready{FeatureID: b.FeatureID, Reason: ReasonNoDesignatedLawyer})
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// actingFor is the user a scheduled run of b acts for. A designated binding fails closed:
// no settings source, or no lawyer named, is ErrNoDesignatedLawyer.
func (s *Bindings) actingFor(ctx context.Context, b Binding) (string, error) {
	if !b.Designated {
		return b.ActingFor, nil
	}
	if s.lawyers == nil {
		return "", fmt.Errorf("feature %s: no settings source: %w", b.FeatureID, ErrNoDesignatedLawyer)
	}
	u, err := s.lawyers.DesignatedLawyer(ctx, b.FeatureID)
	if err != nil {
		return "", fmt.Errorf("feature %s: designated lawyer: %w", b.FeatureID, err)
	}
	if u == "" {
		return "", fmt.Errorf("feature %s: %w", b.FeatureID, ErrNoDesignatedLawyer)
	}
	return u, nil
}
