package roles

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

var _ AgentLimits = (*Table)(nil)

// Modules are the 17 a4 database schemas. A schema is a Go package is an owning module
// (CF-05); gate kinds, effect handlers and event owners are prefixed with one of them.
// Capability ids are permission names, not modules, and keep their own spelling.
var Modules = []string{
	"core", "party", "intake", "conflict", "matter", "docs", "cal", "work", "timekeeping",
	"billing", "trust", "gl", "comms", "agent", "gate", "audit", "platform",
}

// ErrUnknownModule means a gate kind is not prefixed with an a4 module name.
var ErrUnknownModule = errors.New("roles: unknown module")

// GateKindModule returns the owning module of a gate kind slug (<module>.<slug>). A slug
// whose prefix is not an a4 module, such as a former a2 name (documents, deadlines),
// returns ErrUnknownModule.
func GateKindModule(kind string) (string, error) {
	mod, rest, ok := strings.Cut(kind, ".")
	if !ok || rest == "" || !slices.Contains(Modules, mod) {
		return "", fmt.Errorf("gate kind %q: %w", kind, ErrUnknownModule)
	}
	return mod, nil
}

// AgentLevel implements AgentLimits: the weaker of the agent role's and the acting-for
// role's level, capped below Approve.
func (t *Table) AgentLevel(actingFor RoleID, capability string) (Level, error) {
	agent, err := t.Level(Agent, capability)
	if err != nil {
		return None, fmt.Errorf("agent level: %w", err)
	}
	user, err := t.Level(actingFor, capability)
	if err != nil {
		return None, fmt.Errorf("agent level: %w", err)
	}
	// rule: ARC-I-036
	lvl := agent.Min(user)
	// rule: ARC-I-018
	if lvl == Approve {
		lvl = All
	}
	return lvl, nil
}

// AgentCan implements AgentLimits. A need of Approve is never met: the agent only drafts
// and proposes.
func (t *Table) AgentCan(actingFor RoleID, capability string, need Level) (bool, error) {
	if !need.Valid() {
		return false, fmt.Errorf("agent can %s/%s: %w", actingFor, capability, ErrInvalidLevel)
	}
	lvl, err := t.AgentLevel(actingFor, capability)
	if err != nil {
		return false, err
	}
	return lvl.Covers(need), nil
}
