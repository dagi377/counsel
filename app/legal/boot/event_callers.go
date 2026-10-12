package boot

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/outbox"
)

// decidedSetter is the gate engine's hook for every recorded decision (gate.PG).
type decidedSetter interface {
	SetDecided(gate.DecidedHook)
}

// wireGateDecided makes engine g publish gate.decided through em, in the decision's tx, on
// every recorded decision. It reports false when g takes no hook or em is nil.
func wireGateDecided(g gate.Engine, em outbox.Emitter) bool {
	s, ok := g.(decidedSetter)
	if !ok || em == nil {
		return false
	}
	s.SetDecided(gateDecidedPublisher(em))
	return true
}

// gateDecidedPublisher publishes one gate.decided per decision; the decider is the actor.
func gateDecidedPublisher(em outbox.Emitter) gate.DecidedHook {
	return func(ctx context.Context, tx pgx.Tx, d gate.Decided) error {
		// rule: ARC-I-030
		return billing.PublishGateDecided(ctx, em, tx, billing.GateDecided{
			GateID: string(d.GateID), Version: int64(d.Version), Decision: d.Decision,
			DeciderID: d.DeciderID, Final: d.Final, UserID: d.DeciderID,
		})
	}
}
