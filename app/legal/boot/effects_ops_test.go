package boot

import (
	"context"
	"errors"
	"strings"
	"testing"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/registry"
)

// appOpsKinds is how many OpsKinds the registry sees: the core.* ones go to the kernel.
func appOpsKinds() int {
	n := 0
	for _, k := range OpsKinds() {
		if !strings.HasPrefix(k, "core.") {
			n++
		}
	}
	return n
}

// Every ops kind has an effect, none is also an approval-only kind, and with no kernel clock
// the effect fails closed before touching anything.
func TestOpsEffects(t *testing.T) {
	fx, err := opsEffects(&kboot.Kernel{})
	if err != nil {
		t.Fatal(err)
	}
	tests := OpsKinds()
	if len(fx) != len(tests) {
		t.Fatalf("got %d effects, want %d", len(fx), len(tests))
	}
	approval := map[string]bool{}
	for _, k := range ApprovalKinds {
		approval[k] = true
	}
	for _, kind := range tests {
		t.Run(kind, func(t *testing.T) {
			if fx[kind] == nil {
				t.Fatalf("no effect for %s", kind)
			}
			if approval[kind] {
				t.Fatalf("%s is still an approval-only kind", kind)
			}
			g := registry.Approved{GateID: "g-1", Kind: kind, Approvers: []registry.Principal{fxLawyer}}
			if err := fx[kind](context.Background(), nil, g); !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("no clock: err = %v, want ErrMissingDependency", err)
			}
		})
	}
}
