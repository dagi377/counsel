package matter

import (
	"encoding/json"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
)

const (
	tMatter  = "00000000-0000-0000-0000-000000000001"
	tContact = "00000000-0000-0000-0000-000000000002"
	tCheck   = "00000000-0000-0000-0000-000000000003"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	if fx := GateEffects(); len(fx) != 2 {
		t.Fatalf("GateEffects() has %d kinds, want 2", len(fx))
	}
	gatesfake.RunEffectCases(t, []gatesfake.EffectCase{
		{Kind: "matter.party_conflict", Effect: applyPartyConflict, Writes: "approved_by = $5::uuid",
			Payload: partyConflictPayload{MatterID: tMatter, ContactID: tContact, Role: "opposing_party", CheckID: tCheck}},
		{Kind: GateKeyFact, Effect: applyKeyFact, Writes: "superseded_by = id",
			Payload: keyFactPayload{MatterID: tMatter, Field: "date_of_loss", NewValue: json.RawMessage(`"2025-03-14"`)}},
	})
}
