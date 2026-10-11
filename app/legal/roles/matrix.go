package roles

import (
	_ "embed"
	"fmt"
	"sort"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// accessData is a byte-for-byte copy of spec/legal/access.json. go:embed cannot reach spec/
// from app/legal, so the copy lives here and TestAccessDataMatchesSpec keeps it in step.
//
//go:embed access.json
var accessData []byte

// DecideLevel is the least level that may decide a gate on its approver capability: all,
// or approve above it (access.json matrix.levels).
const DecideLevel = All

func init() { add(registerMatrix) }

// registerMatrix registers the spec access matrix: every capability, and every role with
// the capabilities it holds at any level above none. The levels reach the checker through
// ApplyLevels.
func registerMatrix(r registry.Registry) error {
	t, err := ParseAccess(accessData)
	if err != nil {
		return fmt.Errorf("access matrix: %w", err)
	}
	t.Register(r)
	return nil
}

// ApplyLevels gives the checker catalog the spec level order (none < view < own < all <
// approve), the decide level and every built-in role's level on every capability. A check
// then names a minimum level, and a member passes at that level or stronger. Boot runs it
// after the registrations are loaded.
func ApplyLevels(r *identity.Roles) error {
	t, err := ParseAccess(accessData)
	if err != nil {
		return fmt.Errorf("access levels: %w", err)
	}
	return t.ApplyLevels(r)
}

// ApplyLevels is ApplyLevels over t.
func (t *Table) ApplyLevels(r *identity.Roles) error {
	order := make([]string, 0, len(levelRank))
	for l := range levelRank {
		order = append(order, string(l))
	}
	sort.Slice(order, func(i, j int) bool { return levelRank[Level(order[i])] < levelRank[Level(order[j])] })
	r.LevelOrder(order...)
	// rule: ARC-I-018
	// Approver capabilities decide only at all or approve; admin and the agent never decide.
	if err := r.DecideLevel(string(DecideLevel)); err != nil {
		return fmt.Errorf("access levels: %w", err)
	}
	for _, role := range t.roles {
		for _, c := range t.caps {
			lvl, err := t.Level(role, c.ID)
			if err != nil {
				return fmt.Errorf("access levels: %w", err)
			}
			if err := r.Grant(string(role), c.ID, string(lvl)); err != nil {
				return fmt.Errorf("access levels: %w", err)
			}
		}
	}
	return nil
}
