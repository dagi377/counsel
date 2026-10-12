package deadlines

import (
	"testing"

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
	fx := GateEffects()
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
		{Kind: GateCalRulePack, Effect: applyRulePack, Writes: "status = 'retired'",
			Payload: rulePackPayload{Pack: "ON-RCP", Version: 3}},
		{Kind: GateCalDelegation, Effect: applyDelegation, Writes: "default_visibility",
			Payload: delegationPayload{CalendarOwnerID: tOwner, DelegateUserID: tUser, FeedShowsTitles: &yes}},
	})
}
