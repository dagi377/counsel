package boot

import (
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// CoreGateKind records a kernel-owned core.* gate kind whose effect an app supplies
// (KERNEL-CORE). The registry refuses an app id with a kernel prefix, so a core kind never
// goes through Registry.GateKind: the app hands it to the kernel here during Register, and
// apply registers it with the gate engine (gate.CoreRegistrar) after the registry's calls.
// A kind that is not a valid core kind, or is given twice, fails.
func (k *Kernel) CoreGateKind(g registry.GateKind) error {
	if k == nil {
		return fmt.Errorf("core gate kind: no kernel: %w", ErrConfig)
	}
	// rule: ARC-I-013
	if err := gate.ValidateCoreKind(g); err != nil {
		return fmt.Errorf("core gate kind: %w", err)
	}
	if slices.ContainsFunc(k.coreKinds, func(c registry.GateKind) bool { return c.Slug == g.Slug }) {
		return fmt.Errorf("core gate kind %q: %w", g.Slug, gate.ErrDuplicateKind)
	}
	k.coreKinds = append(k.coreKinds, g)
	return nil
}

// CoreGateKinds returns the core.* kinds the apps handed to CoreGateKind, in order.
func (k *Kernel) CoreGateKinds() []registry.GateKind {
	if k == nil {
		return nil
	}
	return slices.Clone(k.coreKinds)
}

// applyCoreKinds registers every recorded core kind with the gate engine.
func applyCoreKinds(k *Kernel) error {
	if len(k.coreKinds) == 0 {
		return nil
	}
	reg, ok := k.Gates.(gate.CoreRegistrar)
	if !ok {
		return fmt.Errorf("core gate kinds: gate engine %T cannot register core kinds: %w", k.Gates, ErrConfig)
	}
	for _, g := range k.coreKinds {
		if err := reg.RegisterCore(g); err != nil {
			return fmt.Errorf("core gate kind %q: %w", g.Slug, err)
		}
	}
	return nil
}
