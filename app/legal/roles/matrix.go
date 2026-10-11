package roles

import (
	_ "embed"
	"fmt"
	"slices"
	"sort"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// accessData is a byte-for-byte copy of spec/legal/access.json. go:embed cannot reach spec/
// from app/legal, so the copy lives here and TestAccessDataMatchesSpec keeps it in step.
//
//go:embed access.json
var accessData []byte

func init() { add(registerMatrix) }

// registerMatrix registers the spec access matrix: every capability, and every role with
// the capabilities it holds. A gate approver capability (some spec gate kind's approver) is
// held only at level all or approve, and never by a reserved role (admin, agent); any
// other capability is held at any level above none.
func registerMatrix(r registry.Registry) error {
	t, err := ParseAccess(accessData)
	if err != nil {
		return fmt.Errorf("access matrix: %w", err)
	}
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		return fmt.Errorf("access matrix: gate kinds: %w", err)
	}
	approvers := map[string]bool{}
	for _, k := range kinds {
		approvers[k.Approver] = true
	}
	t.RegisterGated(r, approvers)
	return nil
}

// reserved roles never hold a gate approver capability: an admin configures the platform
// and an agent only drafts and proposes.
var reserved = map[RoleID]bool{Admin: true, Agent: true}

// RegisterGated registers every capability and, per role, the capabilities it holds. For a
// capability in approvers a role holds it only at level all or above, and a reserved role
// never does; every other capability is held at any level above none.
func (t *Table) RegisterGated(r registry.Registry, approvers map[string]bool) {
	for _, c := range t.caps {
		r.Capability(c)
	}
	for _, role := range t.roles {
		var held []string
		for capID, lvl := range t.grants[role] {
			if lvl == None {
				continue
			}
			// rule: ARC-I-018
			// Viewing or owning items is not approving them; admin and agent never approve.
			if approvers[capID] && (reserved[role] || !lvl.Covers(All)) {
				continue
			}
			held = append(held, capID)
		}
		sort.Strings(held)
		r.Role(registry.Role{ID: string(role), Name: string(role), Capabilities: slices.Clip(held)})
	}
}
