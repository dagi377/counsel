package gatesfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/gates"
)

var opened = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func newFake(t *testing.T) *Fake {
	t.Helper()
	f, err := New(
		gates.Kind{Code: "billing.bill_approval", SpecGate: "G-B2", Approver: "bill.approve"},
		gates.Kind{Code: "trust.withdrawal", SpecGate: "G5", Approver: "trust.sign", CoolingOff: 24 * time.Hour},
	)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNewRejectsSpecCode(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{code: "G-B2", want: gates.ErrSpecCodeAsGateCode},
		{code: "G5", want: gates.ErrSpecCodeAsGateCode},
		{code: "billing.bill_approval", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			_, err := New(gates.Kind{Code: tt.code})
			if !errors.Is(err, tt.want) {
				t.Fatalf("New(%q) err = %v, want %v", tt.code, err, tt.want)
			}
		})
	}
}

func TestKinds(t *testing.T) {
	f := newFake(t)
	ctx := context.Background()
	ks, err := f.Kinds(ctx)
	if err != nil || len(ks) != 2 || ks[0].Code != "billing.bill_approval" {
		t.Fatalf("Kinds = %v, %v", ks, err)
	}
	tests := []struct {
		code string
		want error
	}{
		{code: "trust.withdrawal", want: nil},
		{code: "nope", want: gates.ErrUnknownKind},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if _, err := f.Kind(ctx, tt.code); !errors.Is(err, tt.want) {
				t.Fatalf("Kind(%q) err = %v, want %v", tt.code, err, tt.want)
			}
		})
	}
}

func TestSelfApproval(t *testing.T) {
	f := newFake(t)
	human := gates.Principal{UserID: "u1"}
	tests := []struct {
		name string
		in   gates.SelfApprovalInput
		want bool
	}{
		{name: "sole and on", in: gates.SelfApprovalInput{By: human, ActiveLicensees: 1, SettingEnabled: true}, want: true},
		{name: "setting off", in: gates.SelfApprovalInput{By: human, ActiveLicensees: 1}, want: false},
		{name: "two licensees", in: gates.SelfApprovalInput{By: human, ActiveLicensees: 2, SettingEnabled: true}, want: false},
		{name: "agent", in: gates.SelfApprovalInput{By: gates.Principal{UserID: "a", Agent: true}, ActiveLicensees: 1, SettingEnabled: true}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.SelfApproval(context.Background(), tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Allowed != tt.want || got.Exception != tt.want {
				t.Fatalf("SelfApproval = %+v, want allowed=%v", got, tt.want)
			}
		})
	}
}

func TestCheckDecision(t *testing.T) {
	f := newFake(t)
	prep := gates.Principal{UserID: "prep"}
	agent := gates.Principal{UserID: "run1", Agent: true, ActingFor: "lawyer"}
	signer := gates.Principal{UserID: "signer", Capabilities: []string{"trust.sign", "bill.approve"}}
	tests := []struct {
		name string
		g    gates.Gate
		by   gates.Principal
		now  time.Time
		want error
	}{
		{name: "ok", g: gates.Gate{Code: "billing.bill_approval", Preparer: prep, OpenedAt: opened}, by: signer, now: opened, want: nil},
		{name: "preparer", g: gates.Gate{Code: "billing.bill_approval", Preparer: signer, OpenedAt: opened}, by: signer, now: opened, want: gates.ErrPreparerApproves},
		{name: "agent decides", g: gates.Gate{Code: "billing.bill_approval", Preparer: prep, OpenedAt: opened}, by: agent, now: opened, want: gates.ErrAgentApproves},
		{name: "acting-for user is preparer", g: gates.Gate{Code: "billing.bill_approval", Preparer: agent, OpenedAt: opened}, by: gates.Principal{UserID: "lawyer", Capabilities: []string{"bill.approve"}}, now: opened, want: gates.ErrPreparerApproves},
		{name: "no capability", g: gates.Gate{Code: "billing.bill_approval", Preparer: prep, OpenedAt: opened}, by: gates.Principal{UserID: "x"}, now: opened, want: gates.ErrMissingCapability},
		{name: "cooling off", g: gates.Gate{Code: "trust.withdrawal", Preparer: prep, OpenedAt: opened}, by: signer, now: opened.Add(23 * time.Hour), want: gates.ErrCoolingOff},
		{name: "cooled off", g: gates.Gate{Code: "trust.withdrawal", Preparer: prep, OpenedAt: opened}, by: signer, now: opened.Add(24 * time.Hour), want: nil},
		{name: "unknown kind", g: gates.Gate{Code: "nope", Preparer: prep}, by: signer, now: opened, want: gates.ErrUnknownKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.CheckDecision(context.Background(), tt.g, tt.by, tt.now); !errors.Is(err, tt.want) {
				t.Fatalf("CheckDecision err = %v, want %v", err, tt.want)
			}
		})
	}
}
