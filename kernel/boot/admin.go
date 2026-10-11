package boot

import (
	"fmt"

	"github.com/dagi377/counsel/kernel/identity"
)

// AdminGates builds the kernel's identity admin gates (core.role_change, core.user_activation)
// on the kernel gate engine and AdminStore, with app-supplied effects for other core.* kinds
// (identity.WithCoreEffect). An app calls it during Register to route a core.* effect through
// the kernel: the kernel owns the kind, the app supplies only the handler. The result is kept
// in k.Admin.
func (k *Kernel) AdminGates(opts ...identity.AdminOption) (identity.AdminGates, error) {
	if k == nil {
		return identity.AdminGates{}, fmt.Errorf("kernel admin gates: no kernel: %w", ErrConfig)
	}
	var opener identity.GateOpener
	if k.Gates != nil {
		opener = k.Gates
	}
	ag, err := identity.NewAdminGates(opener, k.AdminStore, opts...)
	if err != nil {
		return identity.AdminGates{}, fmt.Errorf("kernel admin gates: %w", err)
	}
	k.Admin = ag
	return ag, nil
}
