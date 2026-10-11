package gates

import (
	"context"
	"errors"
	"testing"
	"time"
)

// rule: BIL-I-021
func TestCheckDecision(t *testing.T) {
	cat := NewCatalog([]SpecKind{
		{Kind: Kind{Code: "billing.bill_approval", SpecGate: "G-B2", Approver: "billing.approve"}},
		{Kind: Kind{Code: "trust.withdrawal", SpecGate: "G-T1", Approver: "trust.authorize", CoolingOff: 24 * time.Hour}},
	})
	opened := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	later := opened.Add(time.Hour)
	bookkeeper := Principal{UserID: "u-b", Capabilities: []string{"billing.prepare"}}
	lawyerL := Principal{UserID: "u-l", Capabilities: []string{"billing.approve", "trust.authorize"}}
	lawyerM := Principal{UserID: "u-m", Capabilities: []string{"billing.approve", "trust.authorize"}}
	agentForL := Principal{UserID: "agent-1", Agent: true, ActingFor: "u-l"}
	bill := func(p Principal) Gate {
		return Gate{ID: "g1", Code: "billing.bill_approval", Preparer: p, OpenedAt: opened}
	}
	tests := []struct {
		name string
		g    Gate
		by   Principal
		now  time.Time
		err  error
	}{
		{"other lawyer with capability", bill(bookkeeper), lawyerM, later, nil},
		{"preparer approves own bill", bill(lawyerL), lawyerL, later, ErrPreparerApproves},
		{"agent prepared for the decider", bill(agentForL), lawyerL, later, ErrPreparerApproves},
		{"agent prepared, another lawyer decides", bill(agentForL), lawyerM, later, nil},
		{"agent decides", bill(bookkeeper), Principal{UserID: "agent-2", Agent: true, ActingFor: "u-m", Capabilities: lawyerM.Capabilities}, later, ErrAgentApproves},
		{"decider without user", bill(bookkeeper), Principal{Capabilities: lawyerM.Capabilities}, later, errNoDecider},
		{"decider without capability", bill(lawyerL), bookkeeper, later, ErrMissingCapability},
		{"trust inside cooling-off", Gate{ID: "g2", Code: "trust.withdrawal", Preparer: lawyerL, OpenedAt: opened}, lawyerM, later, ErrCoolingOff},
		{"trust after cooling-off", Gate{ID: "g2", Code: "trust.withdrawal", Preparer: lawyerL, OpenedAt: opened}, lawyerM, opened.Add(24 * time.Hour), nil},
		{"unknown kind", Gate{ID: "g3", Code: "billing.nope", Preparer: bookkeeper}, lawyerM, later, ErrUnknownKind},
		{"spec id as code", Gate{ID: "g4", Code: "G-B2", Preparer: bookkeeper}, lawyerM, later, ErrSpecCodeAsGateCode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := cat.CheckDecision(context.Background(), tc.g, tc.by, tc.now)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}
