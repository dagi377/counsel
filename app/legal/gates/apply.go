package gates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/registry"
)

// ErrNotApplied means an approved gate's effect found its subject missing or in a state the
// approval cannot move, so nothing was applied and the deciding tx rolls back.
var ErrNotApplied = errors.New("gates: approved change cannot be applied")

// Execer is the part of pgx.Tx an approval effect uses.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Step is one statement of an approval effect.
type Step struct {
	SQL  string
	Args []any
}

// Approver returns the approving user of g. It refuses a gate with no approver, an agent
// approver, and the preparer (or the user an agent preparer acted for) among the approvers.
func Approver(g registry.Approved) (string, error) {
	if len(g.Approvers) == 0 {
		return "", fmt.Errorf("gate %s: no approver: %w", g.GateID, ErrNotApplied)
	}
	prep := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = g.Preparer.ActingFor
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		// The agent never approves.
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return "", fmt.Errorf("gate %s: %w", g.GateID, ErrAgentApproves)
		}
		// rule: ARC-I-016
		// The preparer never approves, nor the user an agent run prepared it for.
		if prep != "" && a.UserID == prep {
			return "", fmt.Errorf("gate %s: %w", g.GateID, ErrPreparerApproves)
		}
	}
	return g.Approvers[0].UserID, nil
}

// Decode decodes g's payload into v; a payload that does not decode fails closed.
func Decode(g registry.Approved, v any) error {
	if err := json.Unmarshal(g.Payload, v); err != nil {
		return fmt.Errorf("%s gate %s payload: %v: %w", g.Kind, g.GateID, err, ErrNotApplied)
	}
	return nil
}

// Need fails closed with ErrNotApplied naming the first (by name) empty field.
func Need(g registry.Approved, fields map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if fields[name] == "" {
			return fmt.Errorf("%s gate %s payload: no %s: %w", g.Kind, g.GateID, name, ErrNotApplied)
		}
	}
	return nil
}

// Apply runs an approval effect: steps in order, then proof, a statement that must return
// or touch at least one row once the approved change is in place. Steps are written to be
// no-ops when the change is already applied, and proof matches the state the gate left
// (keyed by gate id where the table records it), so a re-run is a no-op and a subject in
// any other state fails closed with ErrNotApplied.
func Apply(ctx context.Context, tx Execer, g registry.Approved, steps []Step, proof Step) error {
	for i, s := range steps {
		if _, err := tx.Exec(ctx, s.SQL, s.Args...); err != nil {
			return fmt.Errorf("%s gate %s step %d: %w", g.Kind, g.GateID, i+1, err)
		}
	}
	tag, err := tx.Exec(ctx, proof.SQL, proof.Args...)
	if err != nil {
		return fmt.Errorf("%s gate %s check: %w", g.Kind, g.GateID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%s gate %s subject %s: %w", g.Kind, g.GateID, g.Subject.ID, ErrNotApplied)
	}
	return nil
}

// ExecEffect is an approval effect over the Execer part of the deciding tx, so it can be
// tested without a database.
type ExecEffect func(ctx context.Context, tx Execer, g registry.Approved) error

// Effect adapts f to a registry.EffectFunc.
func Effect(f ExecEffect) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error { return f(ctx, tx, g) }
}

// Approvers is Approver for a kind that needs n distinct approving users: it refuses what
// Approver refuses and a gate with fewer than n distinct approvers.
func Approvers(g registry.Approved, n int) (string, error) {
	by, err := Approver(g)
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, a := range g.Approvers {
		seen[a.UserID] = true
	}
	if len(seen) < n {
		return "", fmt.Errorf("gate %s: %d distinct approvers, need %d: %w", g.GateID, len(seen), n, ErrNotApplied)
	}
	return by, nil
}
