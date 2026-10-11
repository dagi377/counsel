// CONTRACT: kernel/registry boot validation surface (T-002). Frozen once on main; changes are
// additive only and need a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html
// ("Registered data ... must validate against spec/kernel/*.schema.json, otherwise boot
// fails. Duplicate ids or a kernel prefix make boot fail."). The Registry interface and the
// data types it takes live in registry.go and types.go; bodies live in the T-002 subtasks.

package registry

import "errors"

// Sentinel errors returned by boot validation. Callers match them with errors.Is.
var (
	// ErrDuplicateID means two registrations of one kind share an id.
	ErrDuplicateID = errors.New("registry: duplicate id")
	// ErrKernelPrefix means an app id uses a prefix the kernel reserves.
	ErrKernelPrefix = errors.New("registry: kernel-reserved id prefix")
	// ErrInvalid means registered data fails its spec/kernel schema.
	ErrInvalid = errors.New("registry: data fails its schema")
	// ErrSealed means a registration arrived after boot finished; nothing registers at runtime.
	ErrSealed = errors.New("registry: sealed after boot")
)

// KernelPrefixes are the id prefixes reserved for the kernel. An app id that starts with one
// of them makes boot fail with ErrKernelPrefix.
var KernelPrefixes = []string{"core.", "kernel."}

// Booter is the Registry the kernel hands to an app at boot. After the app's Register
// returns, the kernel calls Seal once: Seal validates everything registered and freezes it.
// A Registry call after Seal is recorded as an ErrSealed failure on the next Seal.
type Booter interface {
	Registry
	// Seal validates all registrations and freezes the registry. It returns every problem
	// found, joined with errors.Join, each wrapping one sentinel error above.
	Seal() error
}
