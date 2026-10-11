package identity

import (
	"context"
	"slices"
	"testing"
)

// TestAgentPrincipalCapabilityIntersection checks that an agent principal holds only the
// capabilities both its acting user and its skill grant hold, so an agent acting for a user
// without an approve capability never holds it.
//
// rule: ARC-I-036
func TestAgentPrincipalCapabilityIntersection(t *testing.T) {
	r, _ := bootHelpdesk(t)
	c := NewChecker(r, StaticAssignments{"tier1": {"agent_tier1"}, "sup": {"supervisor"}})
	ctx := context.Background()
	tests := []struct {
		name      string
		actingFor UserID
		grant     []string
		wantGrant []string
		cap       string
		wantCan   bool
	}{
		{"user lacks approve, grant has it", "tier1", []string{"ticket.read", "ticket.refund_approve"}, []string{"ticket.read"}, "ticket.refund_approve", false},
		{"user lacks close, grant has it", "tier1", []string{"ticket.close"}, nil, "ticket.close", false},
		{"user holds report, grant lacks it", "sup", []string{"ticket.read"}, []string{"ticket.read"}, "ticket.report", false},
		{"user and grant hold report", "sup", []string{"ticket.report"}, []string{"ticket.report"}, "ticket.report", true},
		{"empty grant holds nothing", "sup", nil, nil, "ticket.read", false},
		// rule: ARC-I-018
		{"user holds approve: agent still never approves", "sup", []string{"ticket.refund_approve"}, []string{"ticket.refund_approve"}, "ticket.refund_approve", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := c.AgentPrincipal(ctx, "run1", tc.actingFor, tc.grant)
			if err != nil {
				t.Fatalf("agent principal: %v", err)
			}
			if !slices.Equal(p.Grant, tc.wantGrant) {
				t.Fatalf("grant = %v, want %v", p.Grant, tc.wantGrant)
			}
			got, err := c.Can(ctx, p, tc.cap, Subject{})
			if err != nil || got != tc.wantCan {
				t.Fatalf("can %s = (%v, %v), want %v", tc.cap, got, err, tc.wantCan)
			}
		})
	}
}
