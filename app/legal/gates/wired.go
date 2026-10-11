package gates

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dagi377/counsel/kernel/registry"
)

// RegisterWired registers with r only the kinds in data (spec/gates.json) that effects has a
// handler for, so boot never registers a kind with a nil Effect. It returns the catalog of
// every kind (the read side CheckDecision needs, wired or not) and the slugs of the app kinds
// with no effect yet, sorted.
//
// core.* and kernel.* kinds (registry.KernelPrefixes) belong to the kernel: they are never
// registered here, never reported as unwired, and an effect keyed by one is ignored. An
// effects key that is not a kind in data, or a nil effect, is an error naming the slug.
// RegisterKinds is the register-everything form.
func RegisterWired(r registry.Registry, data []byte, effects map[string]registry.EffectFunc) (*Catalog, []string, error) {
	kinds, err := ParseKinds(data)
	if err != nil {
		return nil, nil, err
	}
	known := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		known[k.Code] = true
	}
	keys := make([]string, 0, len(effects))
	for slug := range effects {
		keys = append(keys, slug)
	}
	slices.Sort(keys)
	for _, slug := range keys {
		if !known[slug] {
			return nil, nil, fmt.Errorf("register wired gates: effect for %q: %w", slug, ErrUnknownKind)
		}
		if effects[slug] == nil {
			return nil, nil, fmt.Errorf("register wired gates: effect for %q is nil", slug)
		}
	}
	var unwired []string
	for _, k := range kinds { // ordered by Code, so unwired comes out sorted
		if kernelKind(k.Code) {
			continue
		}
		effect, ok := effects[k.Code]
		if !ok {
			unwired = append(unwired, k.Code)
			continue
		}
		r.GateKind(k.GateKind(effect))
	}
	return NewCatalog(kinds), unwired, nil
}

// kernelKind reports whether slug carries a kernel-reserved prefix.
func kernelKind(slug string) bool {
	for _, p := range registry.KernelPrefixes {
		if strings.HasPrefix(slug, p) {
			return true
		}
	}
	return false
}
