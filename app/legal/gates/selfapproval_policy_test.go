package gates_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// policyKinds are the kinds the shared self-approval table runs on. They are ordered by
// Code, as NewCatalog requires.
var policyKinds = []gates.SpecKind{
	{Kind: gates.Kind{Code: "core.bulk_export", SpecGate: "GA9", Approver: "admin.export"}, Risk: registry.RiskHigh, MinApprovers: 2, SoleOwnerDelay: 24 * time.Hour},
	{Kind: gates.Kind{Code: "core.mfa_reset", SpecGate: "GA11", Approver: "admin.security"}, Risk: registry.RiskHigh, MinApprovers: 2, SoleOwnerDelay: 24 * time.Hour},
	{Kind: gates.Kind{Code: "matter.renumber", SpecGate: "G-NUM", Approver: "matter.admin"}, Risk: registry.RiskIrreversible, MinApprovers: 2},
	{Kind: gates.Kind{Code: "trust.withdrawal", SpecGate: "G-T1", Approver: "trust.authorize"}, Risk: registry.RiskHigh, MinApprovers: 1},
}

// selfApprovers are the two implementations that must agree: the real policy and the fake
// other packages test against.
func selfApprovers(t *testing.T) map[string]gates.Gates {
	t.Helper()
	f, err := gatesfake.NewSpec(policyKinds...)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]gates.Gates{"catalog": catalogGates{gates.NewCatalog(policyKinds)}, "fake": f}
}

// catalogGates adapts *gates.Catalog, which has no CheckDecision yet (T-007L.4), to the
// SelfApproval part of gates.Gates used here.
type catalogGates struct{ *gates.Catalog }

func (catalogGates) CheckDecision(context.Context, gates.Gate, gates.Principal, time.Time) error {
	return errors.New("not under test")
}

// rule: ARC-I-021
func TestARC_I_021_SelfApprovalPolicySharedByFake(t *testing.T) {
	opened := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	owner := gates.Principal{UserID: "u-own", Capabilities: []string{"admin.export", "admin.security", "matter.admin", "trust.authorize"}}
	other := gates.Principal{UserID: "u-oth", Capabilities: owner.Capabilities}
	agentFor := gates.Principal{UserID: "agent-1", Agent: true, ActingFor: "u-own"}
	gate := func(code string, prep gates.Principal) gates.Gate {
		return gates.Gate{ID: "g-" + code, Code: code, Preparer: prep, OpenedAt: opened}
	}
	in := func(g gates.Gate, by gates.Principal) gates.SelfApprovalInput {
		return gates.SelfApprovalInput{Gate: g, By: by, ActiveLicensees: 1, SettingEnabled: true}
	}
	tests := []struct {
		name      string
		in        gates.SelfApprovalInput
		after     time.Duration // now = opened + after
		allowed   bool
		exception bool
		err       error
	}{
		{"self approves high kind", in(gate("trust.withdrawal", owner), owner), 0, true, true, nil},
		{"agent prepared for decider is self", in(gate("trust.withdrawal", agentFor), owner), 0, true, true, nil},
		{"other decider needs no exception", in(gate("trust.withdrawal", owner), other), 0, true, false, nil},
		{"agent decider", in(gate("trust.withdrawal", owner), gates.Principal{UserID: "agent-2", Agent: true, Capabilities: owner.Capabilities}), 0, false, false, nil},
		{"no user", in(gate("trust.withdrawal", owner), gates.Principal{Capabilities: owner.Capabilities}), 0, false, false, nil},
		{"missing capability", in(gate("trust.withdrawal", owner), gates.Principal{UserID: "u-own"}), 0, false, false, nil},
		{"setting off", gates.SelfApprovalInput{Gate: gate("trust.withdrawal", owner), By: owner, ActiveLicensees: 1}, 0, false, false, nil},
		{"two licensees", gates.SelfApprovalInput{Gate: gate("trust.withdrawal", owner), By: owner, ActiveLicensees: 2, SettingEnabled: true}, 0, false, false, nil},
		{"GA9 at 23h59 waits", in(gate("core.bulk_export", owner), owner), 24*time.Hour - time.Minute, false, false, nil},
		{"GA9 at 24h self-approves", in(gate("core.bulk_export", owner), owner), 24 * time.Hour, true, true, nil},
		{"GA11 at open waits", in(gate("core.mfa_reset", owner), owner), 0, false, false, nil},
		{"GA11 at 24h self-approves", in(gate("core.mfa_reset", owner), owner), 24 * time.Hour, true, true, nil},
		{"GA11 other sole approver also waits", in(gate("core.mfa_reset", owner), other), time.Hour, false, false, nil},
		{"GA9 with no open time waits", in(gates.Gate{ID: "g", Code: "core.bulk_export", Preparer: owner}, owner), 48 * time.Hour, false, false, nil},
		{"irreversible never self-approved", in(gate("matter.renumber", owner), owner), 0, false, false, nil},
		{"irreversible never self-approved later", in(gate("matter.renumber", owner), owner), 30 * 24 * time.Hour, false, false, nil},
		{"irreversible agent-prepared never self-approved", in(gate("matter.renumber", agentFor), owner), 0, false, false, nil},
		{"irreversible other decider", in(gate("matter.renumber", owner), other), 0, true, false, nil},
		{"unknown kind", in(gate("trust.nope", owner), owner), 0, false, false, gates.ErrUnknownKind},
	}
	for impl, g := range selfApprovers(t) {
		for _, tc := range tests {
			t.Run(impl+"/"+tc.name, func(t *testing.T) {
				ctx := clock.WithClock(context.Background(), clockfake.New(opened.Add(tc.after), time.UTC))
				got, err := g.SelfApproval(ctx, tc.in)
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				if got.Allowed != tc.allowed || got.Exception != tc.exception {
					t.Fatalf("got %+v, want allowed=%v exception=%v", got, tc.allowed, tc.exception)
				}
				if tc.err == nil && got.Reason == "" {
					t.Fatal("empty reason")
				}
			})
		}
	}
}
