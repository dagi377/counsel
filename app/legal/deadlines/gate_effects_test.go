package deadlines

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"

	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	tDeadline = "00000000-0000-0000-0000-000000000001"
	tUser     = "00000000-0000-0000-0000-000000000002"
	tOwner    = "00000000-0000-0000-0000-000000000003"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	yes := true
	fx := GateEffects(nil)
	if len(fx) != 7 {
		t.Fatalf("GateEffects() has %d kinds, want 7", len(fx))
	}
	gatesfake.RunEffectCases(t, []gatesfake.EffectCase{
		{Kind: GateCalConfirm, Effect: applyConfirm, SubjectOnly: true, Writes: "status = 'confirmed'",
			Subject: registry.Subject{Kind: subjectDeadline, ID: tDeadline},
			Payload: map[string]any{"matter_id": tUser, "title": "Defence", "due_on": "2026-11-02", "source": map[string]any{}}},
		{Kind: GateCalSecondCheck, Effect: applySecondCheck, Writes: "'second_check'",
			Payload: secondCheckPayload{DeadlineID: tDeadline, DueOn: "2026-11-02"}},
		{Kind: GateCalChange, Effect: applyChange, Writes: "SET due_on = $2::date",
			Payload: changePayload{DeadlineID: tDeadline, DueOn: "2026-11-09", Reason: "consent extension"}},
		{Kind: GateCalMarkMet, Effect: applyMarkMet, Writes: "status = 'met'",
			Payload: markMetPayload{DeadlineID: tDeadline, EvidenceDocumentID: tUser}},
		{Kind: GateCalRelease, Effect: applyRelease, Writes: "status = 'released'",
			Payload: releasePayload{DeadlineID: tDeadline, Reason: "action settled"}},
		{Kind: GateCalRulePack, Effect: noAffected, Writes: "status = 'retired'",
			Payload: rulePackPayload{Pack: "ON-RCP", Version: 3}},
		{Kind: GateCalDelegation, Effect: applyDelegation, Writes: "default_visibility",
			Payload: delegationPayload{CalendarOwnerID: tOwner, DelegateUserID: tUser, FeedShowsTitles: &yes}},
	})
}

// packTx is a rulePackTx over gatesfake.Exec whose QueryRow answers the affected-deadline
// read with rules, holidays and deadlines.
type packTx struct {
	fake                      *gatesfake.Exec
	rules, holidays, affected string
}

func (x *packTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return x.fake.Exec(ctx, sql, args...)
}

func (x *packTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	x.fake.SQL, x.fake.Args = append(x.fake.SQL, sql), append(x.fake.Args, args)
	return packRow{x}
}

type packRow struct{ x *packTx }

func (r packRow) Scan(dest ...any) error {
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = r.x.rules, r.x.holidays, r.x.affected
	return nil
}

// noAffected runs the rule pack effect against a pack no deadline was computed from.
func noAffected(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	return applyRulePack(ctx, &packTx{fake: tx.(*gatesfake.Exec), rules: "[]", holidays: "[]", affected: "[]"}, g, nil)
}

// G7e: approving a pack re-computes its deadlines into cal.change proposals and never moves
// a date itself.
func TestRulePackOpensChanges(t *testing.T) {
	const (
		d1 = "00000000-0000-0000-0000-0000000000d1"
		d2 = "00000000-0000-0000-0000-0000000000d2"
		d3 = "00000000-0000-0000-0000-0000000000d3"
	)
	rules := `[{"id":"defence","trigger":"served","offset_days":20,"day_count":"calendar"},
	           {"id":"reply","trigger":"served","offset_days":10,"day_count":"business","roll":"forward"}]`
	affected := `[{"id":"` + d1 + `","rule_key":"defence","trigger_on":"2026-11-02","due_on":"2026-11-17"},
	              {"id":"` + d2 + `","rule_key":"defence","trigger_on":"2026-11-02","due_on":"2026-11-22"},
	              {"id":"` + d3 + `","rule_key":"gone","trigger_on":"2026-11-02","due_on":"2026-11-30"}]`
	g := registry.Approved{GateID: "00000000-0000-0000-0000-0000000000ff", Kind: GateCalRulePack,
		Payload: json.RawMessage(`{"pack":"ON-RCP","version":4}`), Preparer: gatesfake.AgentRun,
		Approvers: []registry.Principal{gatesfake.Lawyer}}
	tests := []struct {
		name      string
		affected  string
		proof     int64
		nilOpener bool
		wantErr   error
		wantOpen  []string
		wantDue   string
	}{
		{"moved deadline gets a proposal", affected, 1, false, nil, []string{d1}, "2026-11-22"},
		{"nothing affected opens nothing", "[]", 1, false, nil, nil, ""},
		{"no opener fails closed when a change is due", affected, 1, true, gates.ErrNotApplied, nil, ""},
		{"no opener is fine when nothing moves", "[]", 1, true, nil, nil, ""},
		{"pack not activated fails closed", affected, 0, false, gates.ErrNotApplied, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := &packTx{fake: &gatesfake.Exec{StepRows: 1, ProofRows: tt.proof}, rules: rules, holidays: `["2026-11-23"]`, affected: tt.affected}
			var opened []string
			var payloads []changePayload
			open := func(_ context.Context, _ rulePackTx, id string, raw json.RawMessage, prep registry.Principal) error {
				if prep.RunID != gatesfake.AgentRun.RunID {
					t.Errorf("preparer = %+v, want the pack's preparer", prep)
				}
				var p changePayload
				if err := json.Unmarshal(raw, &p); err != nil {
					t.Fatal(err)
				}
				opened, payloads = append(opened, id), append(payloads, p)
				return nil
			}
			if tt.nilOpener {
				open = nil
			}
			err := applyRulePack(context.Background(), x, g, open)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(opened, tt.wantOpen) {
				t.Fatalf("opened = %v, want %v", opened, tt.wantOpen)
			}
			for _, p := range payloads {
				if p.DueOn != tt.wantDue || !strings.Contains(p.Reason, "ON-RCP version 4") {
					t.Fatalf("payload = %+v, want due %s and the pack in the reason", p, tt.wantDue)
				}
			}
			for _, q := range x.fake.SQL {
				if strings.Contains(q, "UPDATE cal.deadline ") {
					t.Fatalf("effect moved a date: %q", q)
				}
			}
		})
	}
}

// cal.change is logged once per gate, so one deadline can change twice by one approver.
func TestChangeUniquePerGate(t *testing.T) {
	tests := []struct {
		name, sql, want string
	}{
		{"change keyed by gate", changeLogSQL, "ON CONFLICT (deadline_id, gate_decision_id) WHERE kind = 'change'"},
		{"confirm keyed by user", confirmLogSQL, "WHERE kind <> 'change' DO NOTHING"},
		{"second check keyed by user", secondCheckSQL, "WHERE kind <> 'change' DO NOTHING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.sql, tt.want) {
				t.Fatalf("sql lacks %q", tt.want)
			}
		})
	}
}
