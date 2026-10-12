package withdraw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/registry"
)

type soleEvidence gates.SoleEvidence

func (s soleEvidence) SoleEvidence(context.Context, pgx.Tx, string, string) (gates.SoleEvidence, error) {
	return gates.SoleEvidence(s), nil
}

// rule: ARC-I-021
// rule: ARC-I-027
func TestARC_I_021_SolePractitionerAuthorizesOwnWithdrawal(t *testing.T) {
	stepUp := execDay.Add(8 * time.Hour)
	sole := soleEvidence{ActiveLicensees: 1, ApproverLicensed: true, StepUpAt: stepUp, Reason: "sole practice: closing funds"}
	two := sole
	two.ActiveLicensees = 2
	noReason := sole
	noReason.Reason = ""
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "prep"}
	tests := []struct {
		name     string
		evidence gates.SoDEvidence // nil: no guard
		approver registry.Principal
		err      error
		audits   int
	}{
		{"sole firm with reason and step-up, audited", sole, user("prep"), nil, 1},
		{"two-licensee firm refused", two, user("prep"), ErrPreparerApproves, 0},
		{"sole firm without stated reason refused", noReason, user("prep"), ErrPreparerApproves, 0},
		{"no guard refuses the preparer", nil, user("prep"), ErrPreparerApproves, 0},
		{"agent refused", sole, agent, ErrNotAuthorized, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, true)
			r.e.Signers = sigs{"prep": true}
			log := auditfake.New("approval_id", "reason")
			if tc.evidence != nil {
				r.e.SoD = &gates.SoDGuard{Evidence: tc.evidence, Audit: log, Now: r.e.Now}
			}
			err := r.e.Apply(context.Background(), nil, r.approved(tc.approver))
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("apply = %v, want %v", err, tc.err)
			}
			want := StatusRequested
			if tc.err == nil {
				want = StatusAuthorized
			}
			if got := r.store.w["w1"].Status; got != want {
				t.Fatalf("status = %s, want %s", got, want)
			}
			evs := log.Events()
			if len(evs) != tc.audits {
				t.Fatalf("audit events = %d, want %d", len(evs), tc.audits)
			}
			for _, e := range evs {
				if e.Action != gates.AuditSolePractitioner || e.ActorID != "prep" || e.Subject.ID != "w1" {
					t.Fatalf("audit event = %+v", e)
				}
			}
		})
	}
}
