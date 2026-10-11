package boot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/gates"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

type oneHit struct{}

func (oneHit) Search(_ context.Context, term string) ([]conflict.Hit, error) {
	return []conflict.Hit{{Term: term, PartyID: "p-1", MatterID: "M-0", Role: "adverse"}}, nil
}

// testClearancePorts registers a lawyer role holding the G2 approver capability and a clerk
// role without it.
func testClearancePorts(t *testing.T) ClearancePorts {
	t.Helper()
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	g2, err := gates.NewCatalog(kinds).Kind(context.Background(), conflict.GateClearance)
	if err != nil {
		t.Fatal(err)
	}
	r := identity.NewRoles()
	r.Capability(registry.Capability{ID: g2.Approver, Description: "clear conflicts"})
	r.Capability(registry.Capability{ID: "matter.read", Description: "read matters"})
	r.Role(registry.Role{ID: "lawyer", Capabilities: []string{"matter.read", g2.Approver}})
	r.Role(registry.Role{ID: "clerk", Capabilities: []string{"matter.read"}})
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return ClearancePorts{
		Search: oneHit{},
		Roles:  r,
		Assignments: identity.StaticAssignments{
			"lawyer-a": {"lawyer"},
			"lawyer-b": {"lawyer"},
			"lawyer-c": {"clerk"}, // a lawyer by profile, but without the G2 capability
		},
	}
}

// bootClearance runs the legal app's Register with Clearance configured and returns it.
func bootClearance(t *testing.T) (*conflict.Checks, *conflict.Clearance) {
	t.Helper()
	p := testClearancePorts(t)
	w := &wiring{opts: Options{Clearance: &p}}
	k := testKernel()
	k.Registry = recorderBooter{&registry.Recorder{}}
	if err := run(context.Background(), k, registerHooks(w)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if w.checks == nil || w.clearance == nil {
		t.Fatal("boot built no Checks or Clearance")
	}
	return w.checks, w.clearance
}

func TestBootClearanceDecide(t *testing.T) {
	preparer := conflict.Actor{UserID: "lawyer-a", IsLawyer: true}
	tests := []struct {
		name     string
		approver conflict.Actor
		want     conflict.Status
		wantErr  []error
	}{
		{"lawyer with the G2 capability clears", conflict.Actor{UserID: "lawyer-b", IsLawyer: true}, conflict.StatusCleared, nil},
		{"lawyer without the G2 capability", conflict.Actor{UserID: "lawyer-c", IsLawyer: true}, conflict.StatusOpen, []error{gates.ErrMissingCapability}},
		{"user with no roles fails closed", conflict.Actor{UserID: "lawyer-z", IsLawyer: true}, conflict.StatusOpen, []error{identity.ErrUnknownUser}},
		{"agent never decides", conflict.Actor{UserID: "agent-1", IsAgent: true, ActingFor: "lawyer-b"}, conflict.StatusOpen, []error{conflict.ErrAgentApproves, gates.ErrAgentApproves}},
		{"preparer never decides", preparer, conflict.StatusOpen, []error{conflict.ErrPreparerApproves}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks, c := bootClearance(t)
			r, err := checks.Check(context.Background(), conflict.Request{MatterID: "M-1", Terms: []string{"Acme"}, Preparer: preparer})
			if err != nil {
				t.Fatal(err)
			}
			if err := checks.Dispose(context.Background(), r.ID, 0, conflict.Actor{UserID: "lawyer-d", IsLawyer: true}, conflict.DispositionNotAConflict, "different entity"); err != nil {
				t.Fatal(err)
			}
			got, err := c.Decide(context.Background(), r.ID, tt.approver, true, "reviewed")
			for _, w := range tt.wantErr {
				if !errors.Is(err, w) {
					t.Fatalf("err = %v, want %v", err, w)
				}
			}
			if tt.wantErr == nil && err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got.Status != tt.want {
				t.Fatalf("status = %s, want %s", got.Status, tt.want)
			}
		})
	}
}

func TestNewClearanceMissingDependency(t *testing.T) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(k *kboot.Kernel, cat **gates.Catalog, p *ClearancePorts)
		want   string
	}{
		{"nil Clock", func(k *kboot.Kernel, _ **gates.Catalog, _ *ClearancePorts) { k.Clock = nil }, "kernel Clock"},
		{"nil Catalog", func(_ *kboot.Kernel, c **gates.Catalog, _ *ClearancePorts) { *c = nil }, "gate Catalog"},
		{"nil Search", func(_ *kboot.Kernel, _ **gates.Catalog, p *ClearancePorts) { p.Search = nil }, "Search"},
		{"nil Roles", func(_ *kboot.Kernel, _ **gates.Catalog, p *ClearancePorts) { p.Roles = nil }, "Roles"},
		{"nil Assignments", func(_ *kboot.Kernel, _ **gates.Catalog, p *ClearancePorts) { p.Assignments = nil }, "Assignments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, cat, p := testKernel(), gates.NewCatalog(kinds), testClearancePorts(t)
			tt.mutate(k, &cat, &p)
			_, _, err := newClearance(k, cat, p)
			if !errors.Is(err, ErrMissingDependency) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want ErrMissingDependency naming %q", err, tt.want)
			}
		})
	}
}

func TestNoClearanceWithoutPorts(t *testing.T) {
	w := &wiring{}
	k := testKernel()
	k.Registry = recorderBooter{&registry.Recorder{}}
	if err := run(context.Background(), k, registerHooks(w)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if w.checks != nil || w.clearance != nil {
		t.Fatal("Clearance built with no ports")
	}
}
