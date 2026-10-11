package docs

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
)

const (
	tMatter  = "00000000-0000-0000-0000-000000000001"
	tVersion = "00000000-0000-0000-0000-000000000002"
	tDoc     = "00000000-0000-0000-0000-000000000003"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	no := false
	if fx := GateEffects(); len(fx) != 5 {
		t.Fatalf("GateEffects() has %d kinds, want 5", len(fx))
	}
	gatesfake.RunEffectCases(t, []gatesfake.EffectCase{
		{Kind: GateEngagementLetter, Effect: applyEngagementLetter, Writes: "status = 'approved'",
			Payload: engagementPayload{MatterID: tMatter, DocumentVersionID: tVersion}},
		{Kind: GateOutgoing, Effect: applyOutgoing, Writes: "status = 'final'",
			Payload: outgoingPayload{MatterID: tMatter, DocumentVersionIDs: []string{tVersion}, Channel: "email"}},
		{Kind: GateFilingPrivilege, Effect: applyFilingPrivilege, Writes: "SET privilege",
			Payload: privilegePayload{DocumentID: tDoc, Privileged: &no}},
		{Kind: GateTemplatePublish, Effect: applyTemplatePublish, Writes: "doc_template_version",
			Payload: templatePayload{TemplateID: tDoc, Version: 2}},
		{Kind: GatePrecedent, Effect: applyPrecedent, Writes: "docs.precedent",
			Payload: precedentPayload{DocumentVersionID: tVersion}},
	})
}
