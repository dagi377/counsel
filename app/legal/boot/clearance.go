package boot

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/gates"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/identity"
)

// The G2 approver's capabilities come from the kernel's generic lookup over registered roles
// and role assignments (Q-CLEARANCE-BOOT). It fails closed for an unknown user or role.
var _ conflict.Capabilities = identity.CapabilityLookup{}

// ClearancePorts are conflict Clearance's dependencies that the composition root's caller
// supplies.
type ClearancePorts struct {
	// Search finds the parties and matters a conflict check term matches.
	Search conflict.Searcher
	// Roles holds the roles and capabilities registered at boot.
	Roles *identity.Roles
	// Assignments says which roles each user holds.
	Assignments identity.RoleAssignments
}

// newClearance builds the conflict Checks and the only conflict.Clearer: Clearance over those
// Checks, deciding G2 against the gate catalog with the approver's capabilities from identity.
func newClearance(k *kboot.Kernel, cat *gates.Catalog, p ClearancePorts) (*conflict.Checks, *conflict.Clearance, error) {
	if k == nil {
		return nil, nil, fmt.Errorf("clearance: kernel: %w", ErrMissingDependency)
	}
	for _, d := range []struct {
		name string
		nil  bool
	}{
		{"kernel Clock", k.Clock == nil},
		{"gate Catalog", cat == nil},
		{"Search", p.Search == nil},
		{"Roles", p.Roles == nil},
		{"Assignments", p.Assignments == nil},
	} {
		if d.nil {
			return nil, nil, fmt.Errorf("clearance: %s: %w", d.name, ErrMissingDependency)
		}
	}
	caps := identity.CapabilityLookup{Roles: p.Roles, Assignments: p.Assignments}
	checks := conflict.NewChecks(p.Search, k.Clock.Now)
	return checks, conflict.NewClearance(checks, cat, caps, k.Clock.Now), nil
}

// buildClearance builds Clearance from the catalog registerGates kept. It runs after
// registerGates; with Options.Clearance nil it builds nothing.
func (w *wiring) buildClearance(_ context.Context, k *kboot.Kernel) error {
	if w.opts.Clearance == nil {
		return nil
	}
	checks, c, err := newClearance(k, w.catalog, *w.opts.Clearance)
	if err != nil {
		return err
	}
	// The run store has no tx, so checks emit in a transaction of their own.
	if k.Emitter != nil {
		setCheckEvents(checks, ownTxEmitter{db: k.DB, em: k.Emitter})
	}
	w.checks, w.clearance = checks, c
	return nil
}
