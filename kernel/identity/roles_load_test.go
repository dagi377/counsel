package identity_test

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestRolesLoad(t *testing.T) {
	rec := &registry.Recorder{}
	rec.Capability(registry.Capability{ID: "thing.read"})
	rec.Capability(registry.Capability{ID: "thing.approve"})
	rec.Role(registry.Role{ID: "member", Capabilities: []string{"thing.read"}})
	rec.GateKind(registry.GateKind{Slug: "thing_gate", Approver: "thing.approve"})
	r := identity.NewRoles()
	r.Load(rec.Calls())
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !r.IsApprover("thing.approve") {
		t.Fatal("gate kind approver not loaded")
	}
	c := identity.NewChecker(r, identity.StaticAssignments{"u1": {"member"}})
	tests := []struct {
		name, user, capability string
		want                   bool
	}{
		{"role holds", "u1", "thing.read", true},
		{"role lacks", "u1", "thing.approve", false},
		{"no role", "u2", "thing.read", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Can(context.Background(), registry.Principal{Kind: registry.PrincipalUser, UserID: tt.user}, tt.capability, registry.Subject{})
			if err != nil || got != tt.want {
				t.Fatalf("Can = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}
