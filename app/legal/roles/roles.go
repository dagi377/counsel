package roles

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/dagi377/counsel/kernel/registry"
)

var _ Matrix = (*Table)(nil)

// Table is the access matrix loaded from access.json (matrix.capabilities, matrix.grants).
type Table struct {
	roles  []RoleID
	caps   []registry.Capability
	known  map[string]bool
	grants map[RoleID]map[string]Level
}

type accessFile struct {
	Roles []struct {
		ID   RoleID `json:"id"`
		Name string `json:"name"`
	} `json:"roles"`
	Matrix struct {
		Capabilities []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"capabilities"`
		Grants map[RoleID]map[string]Level `json:"grants"`
	} `json:"matrix"`
}

// ParseAccess builds a Table from the bytes of access.json. Every grant must name a known
// role, a known capability and a valid level.
func ParseAccess(b []byte) (*Table, error) {
	var f accessFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse access: %w", err)
	}
	t := &Table{known: map[string]bool{}, grants: map[RoleID]map[string]Level{}}
	roleKnown := map[RoleID]bool{}
	for _, r := range f.Roles {
		t.roles = append(t.roles, r.ID)
		roleKnown[r.ID] = true
		t.grants[r.ID] = map[string]Level{}
	}
	for _, c := range f.Matrix.Capabilities {
		t.caps = append(t.caps, registry.Capability{ID: c.ID, Description: c.Name})
		t.known[c.ID] = true
	}
	for role, cells := range f.Matrix.Grants {
		if !roleKnown[role] {
			return nil, fmt.Errorf("parse access: grant for %q: %w", role, ErrUnknownRole)
		}
		for capID, lvl := range cells {
			if !t.known[capID] {
				return nil, fmt.Errorf("parse access: %s/%s: %w", role, capID, ErrUnknownCapability)
			}
			if !lvl.Valid() {
				return nil, fmt.Errorf("parse access: %s/%s: %w", role, capID, ErrInvalidLevel)
			}
			t.grants[role][capID] = lvl
		}
	}
	return t, nil
}

// Level implements Matrix.
func (t *Table) Level(role RoleID, capability string) (Level, error) {
	cells, ok := t.grants[role]
	if !ok {
		return None, fmt.Errorf("level %s: %w", role, ErrUnknownRole)
	}
	if !t.known[capability] {
		return None, fmt.Errorf("level %s/%s: %w", role, capability, ErrUnknownCapability)
	}
	if lvl, ok := cells[capability]; ok {
		return lvl, nil
	}
	return None, nil
}

// Can implements Matrix.
func (t *Table) Can(role RoleID, capability string, need Level) (bool, error) {
	if !need.Valid() {
		return false, fmt.Errorf("can %s/%s: %w", role, capability, ErrInvalidLevel)
	}
	lvl, err := t.Level(role, capability)
	if err != nil {
		return false, err
	}
	return lvl.Covers(need), nil
}

// Register registers every capability and, per role, the capabilities it holds at any level
// above none, with the kernel registry.
func (t *Table) Register(r registry.Registry) {
	for _, c := range t.caps {
		r.Capability(c)
	}
	for _, role := range t.roles {
		var held []string
		for capID, lvl := range t.grants[role] {
			if lvl != None {
				held = append(held, capID)
			}
		}
		sort.Strings(held)
		r.Role(registry.Role{ID: string(role), Name: string(role), Capabilities: slices.Clip(held)})
	}
}
