package identityfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

type wall struct{ hidden string }

func (w wall) Visible(_ context.Context, _ identity.Principal, s identity.Subject) (bool, error) {
	return s.ID != w.hidden, nil
}

func setup() *Fake {
	f := New()
	f.AddCapability(registry.Capability{ID: "desk.view"})
	f.AddCapability(registry.Capability{ID: "desk.approve"})
	f.AddRole(registry.Role{ID: "staff", Capabilities: []string{"desk.view"}})
	f.AddRole(registry.Role{ID: "lead", Capabilities: []string{"desk.view", "desk.approve"}})
	f.Assign("u1", "staff")
	f.Assign("u2", "lead")
	f.AddResolver("ticket", wall{hidden: "t9"})
	return f
}

func TestFakeCan(t *testing.T) {
	ctx := context.Background()
	f := setup()
	staff := identity.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	lead := identity.Principal{Kind: registry.PrincipalUser, UserID: "u2"}
	agentForStaff, _ := f.AgentPrincipal(ctx, "r1", "u1", []string{"desk.view", "desk.approve"})
	agentForLead, _ := f.AgentPrincipal(ctx, "r2", "u2", []string{"desk.view"})
	tests := []struct {
		name    string
		p       identity.Principal
		cap     string
		s       identity.Subject
		want    bool
		wantErr error
	}{
		{"staff view", staff, "desk.view", identity.Subject{}, true, nil},
		{"staff approve", staff, "desk.approve", identity.Subject{}, false, nil},
		{"lead approve", lead, "desk.approve", identity.Subject{}, true, nil},
		{"unknown capability", lead, "desk.nope", identity.Subject{}, false, identity.ErrUnknownCapability},
		{"visible subject", staff, "desk.view", identity.Subject{Kind: "ticket", ID: "t1"}, true, nil},
		{"hidden subject is not visible", lead, "desk.view", identity.Subject{Kind: "ticket", ID: "t9"}, false, identity.ErrNotVisible},
		{"unknown subject kind", staff, "desk.view", identity.Subject{Kind: "x", ID: "1"}, false, identity.ErrUnknownSubjectKind},
		{"agent cannot exceed acting user", agentForStaff, "desk.approve", identity.Subject{}, false, nil},
		{"agent within grant", agentForStaff, "desk.view", identity.Subject{}, true, nil},
		{"agent outside grant", agentForLead, "desk.approve", identity.Subject{}, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Can(ctx, tt.p, tt.cap, tt.s)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Can() err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Can() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFakeAgentPrincipal(t *testing.T) {
	ctx := context.Background()
	f := setup()
	tests := []struct {
		name      string
		run       identity.RunID
		user      identity.UserID
		grant     []string
		wantGrant []string
		wantErr   error
	}{
		{"narrowed", "r1", "u1", []string{"desk.view", "desk.approve"}, []string{"desk.view"}, nil},
		{"full", "r2", "u2", []string{"desk.approve"}, []string{"desk.approve"}, nil},
		{"no run", "", "u1", nil, nil, identity.ErrNoPrincipal},
		{"no user", "r3", "", nil, nil, identity.ErrNoPrincipal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := f.AgentPrincipal(ctx, tt.run, tt.user, tt.grant)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if p.Kind != registry.PrincipalAgent || p.ActingFor != string(tt.user) {
				t.Errorf("principal = %+v", p)
			}
			if len(p.Grant) != len(tt.wantGrant) || (len(p.Grant) > 0 && p.Grant[0] != tt.wantGrant[0]) {
				t.Errorf("Grant = %v, want %v", p.Grant, tt.wantGrant)
			}
		})
	}
}

func TestFakeBind(t *testing.T) {
	tests := []struct {
		name    string
		p       identity.Principal
		wantErr error
	}{
		{"user", identity.Principal{Kind: registry.PrincipalUser, UserID: "u1"}, nil},
		{"empty", identity.Principal{}, identity.ErrNoPrincipal},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.Bind(context.Background(), nil, tt.p); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Bind() = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if n := len(f.Bound()); n != 1 {
		t.Errorf("Bound() has %d entries, want 1", n)
	}
}
