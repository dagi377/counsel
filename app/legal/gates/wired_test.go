package gates_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/registry"
)

// threeKinds is a spec/gates.json with three app kinds and one kernel kind.
const threeKinds = `[
 {"slug":"billing.alpha","spec_id":"G-B1","approver":"billing.approve","risk":"high","min_approvers":1,"ttl":"24h","payload_schema":{"type":"object"}},
 {"slug":"billing.beta","spec_id":"G-B2","approver":"billing.approve","risk":"high","min_approvers":1,"ttl":"24h","payload_schema":{"type":"object"}},
 {"slug":"billing.gamma","spec_id":"G-B3","approver":"billing.approve","risk":"high","min_approvers":1,"ttl":"24h","payload_schema":{"type":"object"}},
 {"slug":"core.role_change","spec_id":"GA1","approver":"core.admin","risk":"high","min_approvers":1,"ttl":"24h","payload_schema":{"type":"object"}}
]`

func noop(context.Context, pgx.Tx, registry.Approved) error { return nil }

func TestRegisterWired(t *testing.T) {
	tests := []struct {
		name        string
		effects     map[string]registry.EffectFunc
		wantSlugs   []string
		wantUnwired []string
		wantErr     error
		errSlug     string
	}{
		{
			name:        "two wired of three",
			effects:     map[string]registry.EffectFunc{"billing.alpha": noop, "billing.gamma": noop},
			wantSlugs:   []string{"billing.alpha", "billing.gamma"},
			wantUnwired: []string{"billing.beta"},
		},
		{
			name:        "none wired",
			effects:     nil,
			wantUnwired: []string{"billing.alpha", "billing.beta", "billing.gamma"},
		},
		{
			name:        "kernel kind effect is not registered by the app",
			effects:     map[string]registry.EffectFunc{"core.role_change": noop, "billing.beta": noop},
			wantSlugs:   []string{"billing.beta"},
			wantUnwired: []string{"billing.alpha", "billing.gamma"},
		},
		{
			name:    "effect for a slug that is not a kind",
			effects: map[string]registry.EffectFunc{"billing.alpah": noop},
			wantErr: gates.ErrUnknownKind,
			errSlug: "billing.alpah",
		},
		{
			name:    "nil effect",
			effects: map[string]registry.EffectFunc{"billing.alpha": nil},
			errSlug: "billing.alpha",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &registry.Recorder{}
			cat, unwired, err := gates.RegisterWired(rec, []byte(threeKinds), tt.effects)
			if tt.errSlug != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errSlug) {
					t.Fatalf("err = %v, want an error naming %q", err, tt.errSlug)
				}
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if n := len(rec.Calls()); n != 0 {
					t.Fatalf("%d registry calls on error, want 0", n)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := rec.Method(registry.MethodGateKind)
			var got []string
			for _, c := range calls {
				g := c.Args[0].(registry.GateKind)
				if g.Effect == nil {
					t.Fatalf("%s registered with nil Effect", g.Slug)
				}
				got = append(got, g.Slug)
			}
			if !slices.Equal(got, tt.wantSlugs) {
				t.Fatalf("registered %v, want %v", got, tt.wantSlugs)
			}
			if !slices.Equal(unwired, tt.wantUnwired) {
				t.Fatalf("unwired = %v, want %v", unwired, tt.wantUnwired)
			}
			// The catalog is the read side: it holds every kind, wired, unwired or kernel.
			kinds, _ := cat.Kinds(context.Background())
			if len(kinds) != 4 {
				t.Fatalf("catalog holds %d kinds, want 4", len(kinds))
			}
		})
	}
}

func TestRegisterKindsUnchanged(t *testing.T) {
	rec := &registry.Recorder{}
	if _, err := gates.RegisterKinds(rec, []byte(threeKinds), nil); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.Method(registry.MethodGateKind)); n != 4 {
		t.Fatalf("RegisterKinds registered %d kinds, want all 4", n)
	}
}

// knownUnwired lists the app gate kinds in spec/gates.json that no feature package has an
// effect for yet. The list may only shrink: wiring a kind fails the test until its slug is
// removed here, and a new kind without an effect fails it until one is written.
var knownUnwired = []string{
	"billing.bill_delivery", // new in spec/gates.json (Q-HTTP-00); the delivery effect is not built yet
	"billing.bill_void",
	"billing.collections_message",
	"billing.payment_allocation",
	"billing.payment_plan",
	"billing.prebill_review",
	"billing.refund",
	"billing.settings_change",
	"billing.trust_fee_withdrawal",
	"cal.calendar_delegation",
	"cal.change",
	"cal.confirm",
	"cal.mark_met",
	"cal.release",
	"cal.rule_pack",
	"cal.second_check",
	"conflict.clearance",
	"conflict.client_id",
	"conflict.consent_text",
	"conflict.id_destruction",
	"conflict.lateral",
	"conflict.wall",
	"docs.engagement_letter",
	"docs.filing_privilege",
	"docs.outgoing",
	"docs.precedent",
	"docs.template_publish",
	"gl.operating_reconciliation",
	"intake.consult_outcome",
	"intake.fee_waiver",
	"intake.lead_decision",
	"intake.open_matter",
	"intake.public_wording",
	"matter.agent_note",
	"matter.apply_template",
	"matter.bulk_change",
	"matter.contact_merge",
	"matter.key_fact",
	"matter.opening",
	"matter.party_conflict",
	"matter.practice_config",
	"matter.renumber",
	"matter.stage_move",
	"matter.widen_access",
	"platform.access_change",
	"platform.closing",
	"platform.escrow_destination",
	"platform.export",
	"platform.file_destruction",
	"platform.guest_access",
	"platform.import_balances",
	"platform.integration_scope",
	"platform.outbound_connector",
	"platform.outgoing_email",
	"platform.payment_provider",
	"platform.portal_message",
	"platform.published_price",
	"platform.research_memo",
	"platform.rule_pack",
	"platform.security_answer",
	"platform.service_credit",
	"platform.skill_autonomy",
	"platform.summary_accuracy",
	"platform.vendor_support",
	"timekeeping.accept",
	"timekeeping.disbursement",
	"timekeeping.fee_arrangement",
	"timekeeping.timesheet",
	"timekeeping.writedown",
	"trust.cash_over_limit",
	"trust.receipt_classification",
	"trust.reconciliation_signoff",
	"trust.regulatory_release",
	"trust.transfer",
	"work.absence",
	"work.automation",
	"work.external_sync",
	"work.output",
	"work.plan",
	"work.workflow_publish",
}

// featureEffects collects every effect the feature packages export, keyed by gate kind. The
// handlers are built over zero-value dependencies: only the keys matter here.
func featureEffects(t *testing.T) map[string]registry.EffectFunc {
	t.Helper()
	all := map[string]registry.EffectFunc{
		withdraw.GateKind:             (&withdraw.Effect{}).Apply,
		credit.GateKindCreditNote:     (&credit.WriteOffEffect{}).Apply,
		credit.GateKindWriteOffSecond: (&credit.WriteOffSecondEffect{}).Apply,
	}
	for _, m := range []map[string]registry.EffectFunc{
		bill.Effects(&bill.ApproveEffect{}),
		signatory.Effects(nil),
	} {
		for slug, fn := range m {
			if _, dup := all[slug]; dup {
				t.Fatalf("two feature effects for %s", slug)
			}
			all[slug] = fn
		}
	}
	return all
}

func TestUnwiredKindsOnlyShrink(t *testing.T) {
	if !slices.IsSorted(knownUnwired) || len(slices.Compact(slices.Clone(knownUnwired))) != len(knownUnwired) {
		t.Fatal("knownUnwired must be sorted with no duplicates")
	}
	_, unwired, err := gates.RegisterWired(&registry.Recorder{}, gates.SpecData(), featureEffects(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range unwired {
		if _, ok := slices.BinarySearch(knownUnwired, slug); !ok {
			t.Errorf("%s has no effect: wire one in its feature package (knownUnwired only shrinks)", slug)
		}
	}
	for _, slug := range knownUnwired {
		if _, ok := slices.BinarySearch(unwired, slug); !ok {
			t.Errorf("%s is wired now or gone from spec/gates.json: remove it from knownUnwired", slug)
		}
	}
}
