package gates

import (
	"context"
	"errors"
	"testing"
)

func selfApprovalCatalog() *Catalog {
	return NewCatalog([]SpecKind{
		{Kind: Kind{Code: "billing.bill_approval", SpecGate: "G-B2", Approver: "billing.approve"}},
		{Kind: Kind{Code: "trust.withdrawal", SpecGate: "G-T1", Approver: "trust.authorize"}},
	})
}

// rule: ARC-I-021
func TestSelfApproval(t *testing.T) {
	lawyer := Principal{UserID: "u-l", Capabilities: []string{"trust.authorize", "billing.approve"}}
	gate := Gate{ID: "g1", Code: "trust.withdrawal", Preparer: Principal{UserID: "u-l"}}
	agentGate := Gate{ID: "g2", Code: "trust.withdrawal", Preparer: Principal{UserID: "agent-1", Agent: true, ActingFor: "u-l"}}
	tests := []struct {
		name      string
		in        SelfApprovalInput
		allowed   bool
		exception bool
		err       error
	}{
		{"sole licensee setting on self-approves", SelfApprovalInput{Gate: gate, By: lawyer, ActiveLicensees: 1, SettingEnabled: true}, true, true, nil},
		{"agent prepared for the sole licensee", SelfApprovalInput{Gate: agentGate, By: lawyer, ActiveLicensees: 1, SettingEnabled: true}, true, true, nil},
		{"two licensees", SelfApprovalInput{Gate: gate, By: lawyer, ActiveLicensees: 2, SettingEnabled: true}, false, false, nil},
		{"no active licensee", SelfApprovalInput{Gate: gate, By: lawyer, ActiveLicensees: 0, SettingEnabled: true}, false, false, nil},
		{"setting off", SelfApprovalInput{Gate: gate, By: lawyer, ActiveLicensees: 1}, false, false, nil},
		{"agent decider", SelfApprovalInput{Gate: gate, By: Principal{UserID: "agent-1", Agent: true, ActingFor: "u-l", Capabilities: lawyer.Capabilities}, ActiveLicensees: 1, SettingEnabled: true}, false, false, nil},
		{"no user", SelfApprovalInput{Gate: gate, By: Principal{Capabilities: lawyer.Capabilities}, ActiveLicensees: 1, SettingEnabled: true}, false, false, nil},
		{"missing capability", SelfApprovalInput{Gate: gate, By: Principal{UserID: "u-l", Capabilities: []string{"billing.approve"}}, ActiveLicensees: 1, SettingEnabled: true}, false, false, nil},
		{"other decider needs no exception", SelfApprovalInput{Gate: gate, By: Principal{UserID: "u-m", Capabilities: lawyer.Capabilities}, ActiveLicensees: 1, SettingEnabled: true}, true, false, nil},
		{"unknown kind", SelfApprovalInput{Gate: Gate{Code: "trust.nope"}, By: lawyer, ActiveLicensees: 1, SettingEnabled: true}, false, false, ErrUnknownKind},
		{"spec id as code", SelfApprovalInput{Gate: Gate{Code: "G-T1"}, By: lawyer, ActiveLicensees: 1, SettingEnabled: true}, false, false, ErrSpecCodeAsGateCode},
	}
	cat := selfApprovalCatalog()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cat.SelfApproval(context.Background(), tc.in)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got.Allowed != tc.allowed || got.Exception != tc.exception {
				t.Fatalf("got %+v, want allowed=%v exception=%v", got, tc.allowed, tc.exception)
			}
			if got.Reason == "" && tc.err == nil {
				t.Fatal("empty reason")
			}
		})
	}
}
