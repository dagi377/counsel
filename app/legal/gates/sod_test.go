package gates

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
)

var sodNow = time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)

// soleDuty is the preparer approving their own work as the firm's one licensee.
func soleDuty() Duty {
	return Duty{What: "trust.withdrawal", ObjectID: "w1", Preparer: "u-sole", Approver: "u-sole",
		ActiveLicensees: 1, ApproverLicensed: true, StepUpAt: sodNow.Add(-time.Minute), Reason: "sole practice: closing today", Now: sodNow}
}

// rule: ARC-I-021
// rule: ARC-I-027
func TestCheckSoD(t *testing.T) {
	with := func(f func(*Duty)) Duty { d := soleDuty(); f(&d); return d }
	second := func(f func(*Duty)) Duty {
		return with(func(d *Duty) {
			d.Second, d.FirstAt, d.StepUpAt = true, sodNow.Add(-time.Hour), sodNow.Add(-time.Minute)
			f(d)
		})
	}
	tests := []struct {
		name string
		d    Duty
		sole bool
		err  error
		also error
	}{
		{"sole firm with reason and step-up", soleDuty(), true, nil, nil},
		{"different approver needs no exception", with(func(d *Duty) { d.Approver = "u-other"; d.ActiveLicensees = 2 }), false, nil, nil},
		{"two-licensee firm refused", with(func(d *Duty) { d.ActiveLicensees = 2 }), false, ErrPreparerApproves, ErrNotSolePractice},
		{"no licensee refused", with(func(d *Duty) { d.ActiveLicensees = 0 }), false, ErrPreparerApproves, ErrNotSolePractice},
		{"approver not the licensee refused", with(func(d *Duty) { d.ApproverLicensed = false }), false, ErrPreparerApproves, ErrNotSolePractice},
		{"agent refused", with(func(d *Duty) { d.Approver = "agent-1"; d.ApproverIsAgent = true }), false, ErrAgentApproves, nil},
		{"agent refused even when not the preparer", with(func(d *Duty) { d.Approver = "agent-2"; d.ApproverIsAgent = true; d.Preparer = "u-other" }), false, ErrAgentApproves, nil},
		{"no stated reason refused", with(func(d *Duty) { d.Reason = "  " }), false, ErrPreparerApproves, ErrSoleReason},
		{"no step-up refused", with(func(d *Duty) { d.StepUpAt = time.Time{} }), false, ErrPreparerApproves, ErrSoleStepUp},
		{"future step-up refused", with(func(d *Duty) { d.StepUpAt = sodNow.Add(time.Minute) }), false, ErrPreparerApproves, ErrSoleStepUp},
		{"no approver refused", with(func(d *Duty) { d.Approver = "" }), false, ErrPreparerApproves, nil},
		{"second confirmation after 1h", second(func(*Duty) {}), true, nil, nil},
		{"second confirmation at 59m refused", second(func(d *Duty) { d.FirstAt = sodNow.Add(-59 * time.Minute) }), false, ErrSoleDelay, nil},
		{"second confirmation with no first refused", second(func(d *Duty) { d.FirstAt = time.Time{} }), false, ErrSoleDelay, nil},
		{"kind delay longer than floor enforced", second(func(d *Duty) { d.MinGap = 24 * time.Hour }), false, ErrSoleDelay, nil},
		{"kind delay met", second(func(d *Duty) { d.MinGap = 24 * time.Hour; d.FirstAt = sodNow.Add(-25 * time.Hour) }), true, nil, nil},
		{"second confirmation reuses first step-up refused", second(func(d *Duty) { d.StepUpAt = d.FirstAt }), false, ErrSoleStepUp, nil},
		{"second confirmation in two-licensee firm refused", second(func(d *Duty) { d.ActiveLicensees = 2 }), false, ErrPreparerApproves, ErrNotSolePractice},
		{"second confirmation by agent refused", second(func(d *Duty) { d.ApproverIsAgent = true }), false, ErrAgentApproves, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckSoD(tc.d)
			if !errors.Is(err, tc.err) || (tc.also != nil && !errors.Is(err, tc.also)) {
				t.Fatalf("err = %v, want %v and %v", err, tc.err, tc.also)
			}
			if tc.err == nil && err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if got.Sole != tc.sole {
				t.Fatalf("sole = %v, want %v", got.Sole, tc.sole)
			}
		})
	}
}

// rule: ARC-I-021
// rule: ARC-I-027
func TestCheckCoolingOffOverride(t *testing.T) {
	k := Kind{Code: "trust.withdrawal", SpecGate: "G-T1", Approver: "trust.authorize", CoolingOff: TrustCoolingOff}
	opened := sodNow.Add(-2 * time.Hour)
	g := Gate{ID: "g1", Code: k.Code, Preparer: Principal{UserID: "u-l"}, OpenedAt: opened}
	agentG := Gate{ID: "g2", Code: k.Code, Preparer: Principal{UserID: "agent-1", Agent: true, ActingFor: "u-l"}, OpenedAt: opened}
	sole := SoleEvidence{ActiveLicensees: 1, ApproverLicensed: true, StepUpAt: sodNow.Add(-time.Minute)}
	firm := SoleEvidence{ActiveLicensees: 2, ApproverLicensed: true, StepUpAt: sodNow.Add(-time.Minute)}
	const closing = "same-day real-estate closing"
	tests := []struct {
		name string
		g    Gate
		o    *Override
		now  time.Time
		sole bool
		err  error
	}{
		{"no override waits", g, nil, sodNow, false, ErrCoolingOff},
		{"after cooling-off no override needed", g, nil, opened.Add(TrustCoolingOff), false, nil},
		{"different lawyer with reason", g, &Override{By: Principal{UserID: "u-m"}, ByIsLawyer: true, SoleEvidence: firm, Reason: closing}, sodNow, false, nil},
		{"sole practitioner preparer with reason", g, &Override{By: Principal{UserID: "u-l"}, ByIsLawyer: true, SoleEvidence: sole, Reason: closing}, sodNow, true, nil},
		{"sole practitioner over agent-prepared gate", agentG, &Override{By: Principal{UserID: "u-l"}, ByIsLawyer: true, SoleEvidence: sole, Reason: closing}, sodNow, true, nil},
		{"preparer in two-lawyer firm refused", g, &Override{By: Principal{UserID: "u-l"}, ByIsLawyer: true, SoleEvidence: firm, Reason: closing}, sodNow, false, ErrPreparerApproves},
		{"sole without step-up refused", g, &Override{By: Principal{UserID: "u-l"}, ByIsLawyer: true, SoleEvidence: SoleEvidence{ActiveLicensees: 1, ApproverLicensed: true}, Reason: closing}, sodNow, false, ErrSoleStepUp},
		{"no documented reason refused", g, &Override{By: Principal{UserID: "u-m"}, ByIsLawyer: true, SoleEvidence: firm}, sodNow, false, ErrSoleReason},
		{"non-lawyer refused", g, &Override{By: Principal{UserID: "u-b"}, SoleEvidence: firm, Reason: closing}, sodNow, false, ErrMissingCapability},
		{"agent refused", g, &Override{By: Principal{UserID: "agent-9", Agent: true, ActingFor: "u-m"}, ByIsLawyer: true, SoleEvidence: firm, Reason: closing}, sodNow, false, ErrAgentApproves},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CheckCoolingOffOr(k, tc.g, tc.now, tc.o)
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err != nil && tc.o != nil && !errors.Is(err, ErrCoolingOff) {
				t.Fatalf("refused override must leave cooling-off in force: %v", err)
			}
			if got.Sole != tc.sole {
				t.Fatalf("sole = %v, want %v", got.Sole, tc.sole)
			}
			// rule: ARC-I-021
			// An allowed override is audited with its documented reason.
			if tc.o != nil && err == nil {
				e := OverrideAuditEvent(tc.g, *tc.o)
				if e.Action != AuditCoolingOffOverride || e.ActorID != tc.o.By.UserID || e.Detail["reason"] != closing || e.Subject.ID != tc.g.ID {
					t.Fatalf("audit event = %+v", e)
				}
			}
		})
	}
}

type fakeEvidence struct {
	ev  SoleEvidence
	err error
}

func (f fakeEvidence) SoleEvidence(context.Context, pgx.Tx, string, string) (SoleEvidence, error) {
	return f.ev, f.err
}

// rule: ARC-I-021
// rule: ARC-I-027
func TestSoDGuardApprove(t *testing.T) {
	sole := SoleEvidence{ActiveLicensees: 1, ApproverLicensed: true, StepUpAt: sodNow.Add(-time.Minute), Reason: "sole practice"}
	two := sole
	two.ActiveLicensees = 2
	self := Duty{What: "billing.bill_approval", ObjectID: "b1", Preparer: "u-l", Approver: "u-l"}
	other := Duty{What: "billing.bill_approval", ObjectID: "b1", Preparer: "u-b", Approver: "u-l"}
	agent := Duty{What: "billing.bill_approval", ObjectID: "b1", Preparer: "u-l", Approver: "agent-1", ApproverIsAgent: true}
	boom := errors.New("db down")
	tests := []struct {
		name   string
		guard  func(*auditfake.Fake) *SoDGuard
		d      Duty
		sole   bool
		audits int
		err    error
	}{
		{"sole firm approves own bill, audited", guardWith(fakeEvidence{ev: sole}), self, true, 1, nil},
		{"two-licensee firm refused, not audited", guardWith(fakeEvidence{ev: two}), self, false, 0, ErrPreparerApproves},
		{"agent refused", guardWith(fakeEvidence{ev: sole}), agent, false, 0, ErrAgentApproves},
		{"other approver passes, not audited", guardWith(fakeEvidence{ev: sole}), other, false, 0, nil},
		{"no stated reason refused", guardWith(fakeEvidence{ev: SoleEvidence{ActiveLicensees: 1, ApproverLicensed: true, StepUpAt: sole.StepUpAt}}), self, false, 0, ErrSoleReason},
		{"evidence error refused", guardWith(fakeEvidence{err: boom}), self, false, 0, boom},
		{"nil guard is strict", func(*auditfake.Fake) *SoDGuard { return nil }, self, false, 0, ErrPreparerApproves},
		{"nil guard lets another approver pass", func(*auditfake.Fake) *SoDGuard { return nil }, other, false, 0, nil},
		{"guard without audit is strict", func(*auditfake.Fake) *SoDGuard { return &SoDGuard{Evidence: fakeEvidence{ev: sole}} }, self, false, 0, ErrPreparerApproves},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log := auditfake.New("approval_id", "reason")
			got, err := tc.guard(log).Approve(context.Background(), nil, "g1", tc.d)
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got.Sole != tc.sole {
				t.Fatalf("sole = %v, want %v", got.Sole, tc.sole)
			}
			evs := log.Events()
			if len(evs) != tc.audits {
				t.Fatalf("audit events = %d, want %d", len(evs), tc.audits)
			}
			for _, e := range evs {
				if e.Action != AuditSolePractitioner || e.ActorID != tc.d.Approver || e.Subject.ID != tc.d.ObjectID {
					t.Fatalf("audit event = %+v", e)
				}
			}
		})
	}
}

func guardWith(ev SoDEvidence) func(*auditfake.Fake) *SoDGuard {
	return func(log *auditfake.Fake) *SoDGuard {
		return &SoDGuard{Evidence: ev, Audit: log, Now: func() time.Time { return sodNow }}
	}
}
