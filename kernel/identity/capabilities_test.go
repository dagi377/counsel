package identity

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

type failingAssignments struct{ err error }

func (f failingAssignments) RolesOf(context.Context, UserID) ([]string, error) { return nil, f.err }

func TestCapabilityLookup(t *testing.T) {
	r := NewRoles()
	for _, c := range []string{"desk.read", "desk.approve", "desk.write"} {
		r.Capability(registry.Capability{ID: c, Description: c})
	}
	r.Role(registry.Role{ID: "clerk", Capabilities: []string{"desk.write", "desk.read"}})
	r.Role(registry.Role{ID: "lead", Capabilities: []string{"desk.read", "desk.approve"}})
	assign := StaticAssignments{
		"u-clerk": {"clerk"},
		"u-both":  {"lead", "clerk"},
		"u-ghost": {"clerk", "nope"},
	}
	errBoom := errors.New("boom")
	tests := []struct {
		name    string
		l       CapabilityLookup
		user    string
		want    []string
		wantErr error
	}{
		{"one role", CapabilityLookup{r, assign}, "u-clerk", []string{"desk.read", "desk.write"}, nil},
		{"union sorted and de-duplicated", CapabilityLookup{r, assign}, "u-both", []string{"desk.approve", "desk.read", "desk.write"}, nil},
		{"unknown user fails closed", CapabilityLookup{r, assign}, "u-none", nil, ErrUnknownUser},
		{"unknown role fails closed", CapabilityLookup{r, assign}, "u-ghost", nil, ErrUnknownRole},
		{"assignment error", CapabilityLookup{r, failingAssignments{errBoom}}, "u-clerk", nil, errBoom},
		{"no roles", CapabilityLookup{Assignments: assign}, "u-clerk", nil, ErrForbidden},
		{"no assignments", CapabilityLookup{Roles: r}, "u-clerk", nil, ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.l.Capabilities(context.Background(), tt.user)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || got != nil {
					t.Fatalf("Capabilities = %v, %v; want nil, %v", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("Capabilities = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestCapabilityLookupReturnsACopy(t *testing.T) {
	r := NewRoles()
	r.Capability(registry.Capability{ID: "desk.read", Description: "read"})
	r.Role(registry.Role{ID: "clerk", Capabilities: []string{"desk.read"}})
	l := CapabilityLookup{r, StaticAssignments{"u": {"clerk"}}}
	got, err := l.Capabilities(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	got[0] = "desk.admin"
	again, _ := l.Capabilities(context.Background(), "u")
	if !slices.Equal(again, []string{"desk.read"}) {
		t.Fatalf("lookup shares the role's slice: %v", again)
	}
}
