package boot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/featureset"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/registry"
)

// fakeExec records the statement an approval runs and answers rows affected.
type fakeExec struct {
	rows int64
	err  error
	sql  string
	args []any
}

func (f *fakeExec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.sql, f.args = sql, args
	if f.err != nil {
		return pgconn.CommandTag{}, f.err
	}
	return pgconn.NewCommandTag("UPDATE " + itoa(f.rows)), nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

var (
	fxLawyer   = registry.Principal{Kind: registry.PrincipalUser, UserID: "u-fxLawyer"}
	fxPreparer = registry.Principal{Kind: registry.PrincipalUser, UserID: "u-prep"}
	agentRun   = registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: "u-prep"}
)

func approved(kind, subjectKind string, prep registry.Principal, approvers ...registry.Principal) registry.Approved {
	return registry.Approved{GateID: "g-1", Kind: kind, Subject: registry.Subject{Kind: subjectKind, ID: "s-1"},
		Preparer: prep, Approvers: approvers}
}

func TestApprovalEffect(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name    string
		g       registry.Approved
		rows    int64
		execErr error
		wantSQL string // substring of the statement run; empty means none runs
		wantErr error
	}{
		// rule: ARC-I-019
		{"agent never approves", approved("intake.consult_outcome", SubjectDocumentDraft, fxPreparer, agentRun), 1, nil, "", gates.ErrAgentApproves},
		{"fxPreparer never approves", approved("intake.consult_outcome", SubjectDocumentDraft, fxPreparer, fxPreparer), 1, nil, "", gates.ErrPreparerApproves},
		{"run's acting user never approves", approved("timekeeping.fee_arrangement", SubjectDocumentDraft, agentRun, fxPreparer), 1, nil, "", gates.ErrPreparerApproves},
		{"no approver", approved("intake.consult_outcome", SubjectDocumentDraft, fxPreparer), 1, nil, "", ErrNotApprovable},
		{"tool-gap draft approved", approved("intake.consult_outcome", SubjectDocumentDraft, agentRun, fxLawyer), 1, nil, "docs.draft SET status = 'approved'", nil},
		{"summary accuracy draft approved", approved("platform.summary_accuracy", SubjectDocumentDraft, agentRun, fxLawyer), 1, nil, "docs.draft", nil},
		{"fee arrangement draft approved", approved("timekeeping.fee_arrangement", SubjectDocumentDraft, agentRun, fxLawyer), 1, nil, "docs.draft", nil},
		{"G-B7 collections message stamped and queued", approved("billing.collections_message", agent.SubjectEmailDraft, agentRun, fxLawyer), 1, nil, "approval_id = $2::uuid", nil},
		{"agent note confirmed", approved("matter.agent_note", agent.SubjectAgentNote, agentRun, fxLawyer), 1, nil, "confirmed_by = $2::uuid", nil},
		{"discarded draft fails closed", approved("intake.consult_outcome", SubjectDocumentDraft, agentRun, fxLawyer), 0, nil, "docs.draft", ErrNotApprovable},
		{"message approved by another gate fails closed", approved("billing.collections_message", agent.SubjectEmailDraft, agentRun, fxLawyer), 0, nil, "comm_item", ErrNotApprovable},
		{"store error wrapped", approved("intake.consult_outcome", SubjectDocumentDraft, agentRun, fxLawyer), 0, boom, "docs.draft", boom},
		{"other subject recorded by the decision", approved("conflict.wall", "conflict_check", fxPreparer, fxLawyer), 0, nil, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeExec{rows: tt.rows, err: tt.execErr}
			err := approve(context.Background(), tx, tt.g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSQL == "" && tx.sql != "" {
				t.Fatalf("ran %q, want no statement", tx.sql)
			}
			if !strings.Contains(tx.sql, tt.wantSQL) {
				t.Fatalf("ran %q, want it to contain %q", tx.sql, tt.wantSQL)
			}
		})
	}
}

// Every statement matches a row the same gate already approved, so a re-run changes nothing
// and still succeeds (idempotent).
func TestApprovalEffectIdempotent(t *testing.T) {
	tests := []struct {
		name, subject, want string
	}{
		{"draft", SubjectDocumentDraft, "'approved')"},
		{"note", agent.SubjectAgentNote, "confirmed_by = $2::uuid)"},
		{"message", agent.SubjectEmailDraft, "approval_id = $2::uuid)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeExec{rows: 1}
			g := approved("intake.consult_outcome", tt.subject, agentRun, fxLawyer)
			for i := 0; i < 2; i++ {
				if err := approve(context.Background(), tx, g); err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
			}
			if !strings.Contains(tx.sql, tt.want) {
				t.Fatalf("statement %q does not re-match an approved row (%q)", tx.sql, tt.want)
			}
		})
	}
}

func TestWithApprovalEffectsKeepsDedicated(t *testing.T) {
	fx, err := withApprovalEffects(map[string]registry.EffectFunc{"intake.consult_outcome": noopEffect})
	if err != nil {
		t.Fatal(err)
	}
	if len(fx) != len(ApprovalKinds) {
		t.Fatalf("got %d effects, want %d", len(fx), len(ApprovalKinds))
	}
	if err := fx["intake.consult_outcome"](context.Background(), nil, registry.Approved{}); err != nil {
		t.Fatalf("dedicated effect replaced: %v", err)
	}
}

// Every gate kind in spec/gates.json and every kind a feature Def declares resolves in the
// production gate catalogue: app kinds through the registry, core.* kinds through the kernel
// (KERNEL-CORE). Only the core kinds no effect exists for yet are left out.
func TestProductionRegistersEveryDeclaredGateKind(t *testing.T) {
	key := trust.SealKey{ID: 1, Key: make([]byte, 32)}
	c := compose(t, legalFlagNames(t), Options{Production: &Production{SealKey: &key}})
	registered := map[string]bool{}
	for _, call := range c.boot.Method(registry.MethodGateKind) {
		registered[call.Args[0].(registry.GateKind).Slug] = true
	}
	for _, g := range c.k.CoreGateKinds() {
		registered[g.Slug] = true
	}
	want := map[string]string{}
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		want[k.Code] = "spec/gates.json"
	}
	set, err := featureset.App()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range set.All() {
		for _, k := range d.GateKinds {
			want[k] = d.ID
		}
	}
	// Kernel admin kinds whose effect no app or kernel store supplies yet (Q-GE2-OPS-CORE).
	noEffect := map[string]bool{
		"core.role_change": true, "core.user_activation": true, "core.temp_grant": true,
		"core.wall_lift": true, "core.break_glass_review": true, "core.review_close": true,
		"core.bulk_export": true, "core.sign_in_settings": true, "core.mfa_reset": true,
		"core.vendor_support": true, "core.authority_limit": true,
	}
	for _, slug := range []string{"core.offboarding", "core.matter_visibility", "core.trust_signatory",
		"trust.receipt_classification", "billing.refund"} {
		if !registered[slug] {
			t.Errorf("gate kind %s is not in the production catalogue", slug)
		}
	}
	for slug, from := range want {
		if noEffect[slug] || strings.HasPrefix(slug, "kernel.") {
			continue
		}
		if !registered[slug] {
			t.Errorf("gate kind %s (from %s) is not in the production catalogue", slug, from)
		}
	}
}
