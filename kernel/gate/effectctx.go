package gate

import (
	"context"
	"errors"
)

// ErrOutsideEffect is returned by RequireEffect when the caller is not inside a gate effect.
var ErrOutsideEffect = errors.New("gate: effect-owned write outside a gate effect")

type effectKey struct{}

// WithEffect marks ctx as running the effect of gate id. Only the effect runner calls it,
// right before it hands ctx to a kind's effect handler.
func WithEffect(ctx context.Context, id GateID) context.Context {
	return context.WithValue(ctx, effectKey{}, id)
}

// InEffect reports the gate whose effect ctx runs in, if any.
func InEffect(ctx context.Context) (GateID, bool) {
	id, ok := ctx.Value(effectKey{}).(GateID)
	return id, ok && id != ""
}

// RequireEffect fails with ErrOutsideEffect unless ctx runs inside a gate effect. Service
// methods that write gate-owned state, and their fakes, call it first.
func RequireEffect(ctx context.Context) error {
	// rule: ARC-I-012
	if _, ok := InEffect(ctx); !ok {
		return ErrOutsideEffect
	}
	return nil
}
