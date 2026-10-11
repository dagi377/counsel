package decider

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

func TestCheck(t *testing.T) {
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	agent := registry.Principal{Kind: registry.PrincipalAgent, ActingFor: "u1"}
	tests := []struct {
		name string
		g    registry.Approved
		want string
		err  error
	}{
		{"other user approves", registry.Approved{Preparer: user("u1"), Approvers: []registry.Principal{user("u2")}}, "u2", nil},
		{"no approver", registry.Approved{Preparer: user("u1")}, "", ErrNoApprover},
		// rule: ARC-I-019
		{"agent approver", registry.Approved{Preparer: user("u1"), Approvers: []registry.Principal{agent}}, "", ErrAgentApproves},
		// rule: ARC-I-027
		{"preparer approves", registry.Approved{Preparer: user("u1"), Approvers: []registry.Principal{user("u1")}}, "", ErrPreparerApproves},
		{"agent preparer's user approves", registry.Approved{Preparer: agent, Approvers: []registry.Principal{user("u1")}}, "", ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Check(tt.g)
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) || got != tt.want {
				t.Fatalf("Check = %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}
