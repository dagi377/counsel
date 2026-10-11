package rolesfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/roles"
)

func newFake() *Fake {
	return New(
		roles.Grant{Role: roles.Lawyer, Capability: "billing.approve", Level: roles.Approve},
		roles.Grant{Role: roles.Clerk, Capability: "billing.approve", Level: roles.View},
		roles.Grant{Role: roles.Agent, Capability: "billing.approve", Level: roles.All},
		roles.Grant{Role: roles.Clerk, Capability: "matters.view", Level: roles.Own},
		roles.Grant{Role: roles.Agent, Capability: "matters.view", Level: roles.All},
	)
}

func TestCan(t *testing.T) {
	f := newFake()
	tests := []struct {
		name    string
		role    roles.RoleID
		cap     string
		need    roles.Level
		want    bool
		wantErr error
	}{
		{"lawyer approves", roles.Lawyer, "billing.approve", roles.Approve, true, nil},
		{"clerk only views", roles.Clerk, "billing.approve", roles.Approve, false, nil},
		{"clerk view covers view", roles.Clerk, "billing.approve", roles.View, true, nil},
		{"ungranted is none", roles.Owner, "billing.approve", roles.View, false, nil},
		{"none always covered", roles.Owner, "billing.approve", roles.None, true, nil},
		{"unknown role", roles.RoleID("intern"), "billing.approve", roles.View, false, roles.ErrUnknownRole},
		{"unknown capability", roles.Lawyer, "billing.nope", roles.View, false, roles.ErrUnknownCapability},
		{"invalid need", roles.Lawyer, "billing.approve", roles.Level("god"), false, roles.ErrInvalidLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Can(tt.role, tt.cap, tt.need)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Can = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentNeverApproves(t *testing.T) {
	f := newFake()
	tests := []struct {
		name      string
		actingFor roles.RoleID
		cap       string
		want      roles.Level
		wantErr   error
	}{
		{"acting for approver is capped at all", roles.Lawyer, "billing.approve", roles.All, nil},
		{"acting for clerk gets clerk level", roles.Clerk, "billing.approve", roles.View, nil},
		{"acting for clerk on own matters", roles.Clerk, "matters.view", roles.Own, nil},
		{"agent acting for agent", roles.Agent, "matters.view", roles.None, roles.ErrUnknownRole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.AgentLevel(tt.actingFor, tt.cap)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("AgentLevel = %v, want %v", got, tt.want)
			}
			if ok, _ := f.AgentCan(tt.actingFor, tt.cap, roles.Approve); ok {
				t.Fatal("agent can approve")
			}
		})
	}
}

func TestVisible(t *testing.T) {
	f := newFake()
	f.Wall("u1", "m1")
	tests := []struct {
		name, user, matter string
		want               bool
	}{
		{"walled", "u1", "m1", false},
		{"other matter", "u1", "m2", true},
		{"other user", "u2", "m1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Visible(context.Background(), tt.user, tt.matter)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Visible = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetGrantRejectsInvalidLevel(t *testing.T) {
	err := New().SetGrant(roles.Grant{Role: roles.Owner, Capability: "x.y", Level: "max"})
	if !errors.Is(err, roles.ErrInvalidLevel) {
		t.Fatalf("err = %v, want ErrInvalidLevel", err)
	}
}
