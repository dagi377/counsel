package conflict

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	tCheck = "00000000-0000-0000-0000-000000000001"
	tID    = "00000000-0000-0000-0000-000000000002"
	tOther = "00000000-0000-0000-0000-000000000003"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	if fx := GateEffects(); len(fx) != 6 {
		t.Fatalf("GateEffects() has %d kinds, want 6", len(fx))
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
		{Kind: GateIDDestruction, Effect: applyIDDestruction, Writes: "UPDATE party.id_document", TwoApprovers: true,
			Payload: idDestructionPayload{ContactID: tID, RecordIDs: []string{tCheck, tOther, tCheck}}},
	})
}

// G14 redacts and stamps the ID artefacts; it never deletes the identity record, so the
// conflict-check history remains.
func TestIDDestructionKeepsRecord(t *testing.T) {
	g := registry.Approved{GateID: "00000000-0000-0000-0000-0000000000ff", Kind: GateIDDestruction,
		Preparer: gatesfake.Preparer, Approvers: []registry.Principal{gatesfake.Lawyer, gatesfake.Partner}}
	tests := []struct {
		name    string
		payload string
		wantErr error
		wantIDs []string
	}{
		{"records deduplicated", `{"contact_id":"` + tID + `","record_ids":["` + tOther + `","` + tCheck + `","` + tOther + `"]}`, nil, []string{tCheck, tOther}},
		{"no records", `{"contact_id":"` + tID + `","record_ids":[]}`, gates.ErrNotApplied, nil},
		{"no contact", `{"record_ids":["` + tCheck + `"]}`, gates.ErrNotApplied, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.Payload = json.RawMessage(tt.payload)
			x := &gatesfake.Exec{StepRows: 1, ProofRows: 1}
			err := applyIDDestruction(context.Background(), x, g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			for _, q := range x.SQL {
				if strings.Contains(strings.ToUpper(q), "DELETE") {
					t.Fatalf("statement deletes: %q", q)
				}
			}
			if tt.wantIDs == nil {
				return
			}
			if !strings.Contains(x.SQL[1], "destroyed_at = now()") || !strings.Contains(x.SQL[0], "number_ciphertext = '\\x'") {
				t.Fatalf("statements = %q, want redaction and a destroyed_at stamp", x.SQL)
			}
			if got := x.Args[0][0].([]string); !slices.Equal(got, tt.wantIDs) {
				t.Fatalf("record ids = %v, want %v", got, tt.wantIDs)
			}
		})
	}
}
