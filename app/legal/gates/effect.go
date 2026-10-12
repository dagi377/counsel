package gates

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/kernel/registry"
)

// ErrNotApprovable means an approved gate's subject is missing, its payload is not usable, or
// the subject is in a state its approval cannot move (decided differently under another gate).
// It is ErrNotApplied under the name the GE2-ops effects use, so both match either name. Execer
// and Approver are apply.go's.
var ErrNotApprovable = ErrNotApplied

// DecodePayload decodes g's payload into v; a payload that does not decode is ErrNotApprovable.
func DecodePayload(g registry.Approved, v any) error {
	if err := json.Unmarshal(g.Payload, v); err != nil {
		return fmt.Errorf("approve %s (gate %s): payload: %w", g.Kind, g.GateID, ErrNotApprovable)
	}
	return nil
}

// ExecOne runs one idempotent effect statement: it matches the row in the state the approval
// moves it from and in the state the same approval already left it in, so a re-run succeeds
// and changes nothing. No row matched is ErrNotApprovable.
func ExecOne(ctx context.Context, tx Execer, g registry.Approved, sql string, args ...any) error {
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("approve %s %s (gate %s): %w", g.Kind, g.Subject.ID, g.GateID, ErrNotApprovable)
	}
	return nil
}

// CoreKinds are the kernel registrations of the core.* kinds in data (spec/gates.json) that
// effects has a handler for. RegisterWired skips core kinds; boot hands these to the kernel
// (kboot.Kernel.CoreGateKind), which registers them (KERNEL-CORE).
func CoreKinds(data []byte, effects map[string]registry.EffectFunc) ([]registry.GateKind, error) {
	kinds, err := ParseKinds(data)
	if err != nil {
		return nil, err
	}
	var out []registry.GateKind
	for _, k := range kinds { // ordered by Code
		if !strings.HasPrefix(k.Code, "core.") {
			continue
		}
		if fn := effects[k.Code]; fn != nil {
			out = append(out, k.GateKind(fn))
		}
	}
	return out, nil
}
