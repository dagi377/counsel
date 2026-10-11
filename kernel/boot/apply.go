package boot

import (
	"errors"
	"fmt"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"
)

// ErrNotSealed means apply ran on a kernel whose registry has not passed Seal.
var ErrNotSealed = errors.New("boot: registry not sealed")

// callLister is the part of a Booter that apply reads: the recorded registrations in order.
// registry.Boot provides it through its embedded Recorder.
type callLister interface {
	Calls() []registry.Call
}

// apply hands every sealed registration to the kernel engine that owns it, walking the
// registry's calls once in order: GateKind to Gates, Report to Reports, Routes to Server and
// EventType to Events. It runs only after a successful Seal; on an unsealed kernel it changes
// nothing. The first engine error stops it.
func apply(k *Kernel) error {
	if k == nil || k.Registry == nil || !k.sealed {
		return fmt.Errorf("apply registrations: %w", ErrNotSealed)
	}
	lister, ok := k.Registry.(callLister)
	if !ok {
		return fmt.Errorf("apply registrations: registry %T cannot list its calls: %w", k.Registry, ErrConfig)
	}
	for i, c := range lister.Calls() {
		if err := applyCall(k, c); err != nil {
			return fmt.Errorf("apply registration %d (%s): %w", i, c.Method, err)
		}
	}
	// KERNEL-CORE: the kernel registers the core.* kinds apps supplied effects for.
	return applyCoreKinds(k)
}

// applyCall hands one call to its engine. Methods no engine consumes yet are skipped.
func applyCall(k *Kernel, c registry.Call) error {
	switch c.Method {
	case registry.MethodGateKind:
		g, ok := arg[registry.GateKind](c, 0)
		if !ok {
			return badArgs(c)
		}
		if k.Gates == nil {
			return fmt.Errorf("gate kind %q: no gate engine: %w", g.Slug, ErrConfig)
		}
		return k.Gates.Register(g)
	case registry.MethodReport:
		d, ok := arg[registry.ReportDef](c, 0)
		if !ok {
			return badArgs(c)
		}
		if k.Reports == nil {
			return fmt.Errorf("report %q: no report engine: %w", d.ID, ErrConfig)
		}
		return k.Reports.Register(d)
	case registry.MethodRoutes:
		fn, ok := arg[func(chi.Router)](c, 0)
		if !ok || fn == nil {
			return badArgs(c)
		}
		if k.Server == nil {
			return fmt.Errorf("routes: no server: %w", ErrConfig)
		}
		return k.Server.Mount(server.Mounter(fn))
	case registry.MethodEventType:
		name, ok1 := arg[string](c, 0)
		schema, ok2 := arg[[]byte](c, 1)
		if !ok1 || !ok2 {
			return badArgs(c)
		}
		if k.Events == nil {
			return fmt.Errorf("event type %q: no event types: %w", name, ErrConfig)
		}
		return k.Events.Register(webhook.EventType{Name: name, Schema: schema})
	}
	return nil
}

// arg returns c.Args[i] as T.
func arg[T any](c registry.Call, i int) (T, bool) {
	var zero T
	if i >= len(c.Args) {
		return zero, false
	}
	v, ok := c.Args[i].(T)
	return v, ok
}

func badArgs(c registry.Call) error {
	return fmt.Errorf("%s: unexpected arguments: %w", c.Method, ErrConfig)
}
