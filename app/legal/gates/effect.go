package gates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/registry"
)

// ErrNotApprovable means an approved gate's subject is missing, its payload is not usable, or
// the subject is in a state its approval cannot move (decided differently under another gate).
var ErrNotApprovable = errors.New("gates: gate subject cannot be approved")

// Execer is the part of pgx.Tx a per-kind effect uses, so tests run without a database.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Approver checks who decided g and returns the first approver's user id. It refuses an
// approval with no approver, an agent approver, and the preparer (or the user an agent
// preparer acted for) among the approvers. The kernel refuses these when deciding; every
// per-kind effect fails closed on them as well.
func Approver(g registry.Approved) (string, error) {
	if len(g.Approvers) == 0 {
		return "", fmt.Errorf("approve %s (gate %s): no approver: %w", g.Kind, g.GateID, ErrNotApprovable)
	}
	prep := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = g.Preparer.ActingFor
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return "", fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, ErrAgentApproves)
		}
		if prep != "" && a.UserID == prep {
			return "", fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, ErrPreparerApproves)
		}
	}
	return g.Approvers[0].UserID, nil
}

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
