package conflict

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
)

const (
	tCheck = "00000000-0000-0000-0000-000000000001"
	tID    = "00000000-0000-0000-0000-000000000002"
	tOther = "00000000-0000-0000-0000-000000000003"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	if fx := GateEffects(); len(fx) != 5 {
		t.Fatalf("GateEffects() has %d kinds, want 5", len(fx))
	}
	gatesfake.RunEffectCases(t, []gatesfake.EffectCase{
		{Kind: GateClearance, Effect: applyClearance, Writes: "INSERT INTO conflict.clearance",
			Payload: map[string]any{"check_id": tCheck, "parties": []string{"Acme"}}},
		{Kind: GateConsentText, Effect: applyConsentText, Writes: "status = 'final'",
			Payload: consentTextPayload{CheckID: tCheck, DocumentVersionID: tID}},
		{Kind: GateWall, Effect: applyWall, Writes: "status = 'active'",
			Payload: wallPayload{WallID: tID, MatterID: tOther, ScreenedUserIDs: []string{tOther}, Action: "activate"}},
		{Kind: GateWall, Effect: applyWall, Writes: "status = 'lifted'",
			Payload: wallPayload{WallID: tID, MatterID: tOther, ScreenedUserIDs: []string{tOther}, Action: "lift"}},
		{Kind: GateLateral, Effect: applyLateral, Writes: "conflict.lateral_disclosure",
			Payload: lateralPayload{UserID: tOther, CheckID: tCheck}},
		{Kind: GateClientID, Effect: applyClientID, Writes: "status = 'verified'",
			Payload: clientIDPayload{ContactID: tID, Method: "government_photo_id", VerifiedOn: "2026-10-09"}},
	})
}
