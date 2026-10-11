package matter

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
)

const (
	tMatter   = "00000000-0000-0000-0000-000000000001"
	tContact  = "00000000-0000-0000-0000-000000000002"
	tCheck    = "00000000-0000-0000-0000-000000000003"
	tMatter2  = "00000000-0000-0000-0000-000000000004"
	tContact2 = "00000000-0000-0000-0000-000000000005"
	tArea     = "00000000-0000-0000-0000-000000000006"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	if fx := GateEffects(); len(fx) != 8 {
		t.Fatalf("GateEffects() has %d kinds, want 8", len(fx))
	}
	yes := true
	gatesfake.RunEffectCases(t, []gatesfake.EffectCase{
		{Kind: "matter.party_conflict", Effect: applyPartyConflict, Writes: "approved_by = $5::uuid",
			Payload: partyConflictPayload{MatterID: tMatter, ContactID: tContact, Role: "opposing_party", CheckID: tCheck}},
		{Kind: GateKeyFact, Effect: applyKeyFact, Writes: "superseded_by = id",
			Payload: keyFactPayload{MatterID: tMatter, Field: "date_of_loss", NewValue: json.RawMessage(`"2025-03-14"`)}},
		// rule: MIG-014
		{Kind: GateContactMerge, Effect: applyContactMerge, Writes: "state = 'merged'",
			Payload: ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "merge", SurvivorID: tContact}},
		{Kind: GateContactMerge, Effect: applyContactMerge, Writes: "state = 'archived'",
			Payload: ContactMergePayload{ContactIDs: []string{tContact2}, Action: "delete"}},
		{Kind: GateContactMerge, Effect: applyContactMerge, Writes: "INSERT INTO party.contact_relationship",
			Payload: ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "relate", RelationshipKind: "spouse"}},
		{Kind: GatePracticeConfig, Effect: applyPracticeConfig, Writes: "config_version = config_version + 1",
			Payload: PracticeConfigPayload{PracticeArea: tArea, Change: map[string]json.RawMessage{"default_billing_method": json.RawMessage(`"flat"`)}}},
		{Kind: GateBulkChange, Effect: applyBulkChange, Writes: "responsible_lawyer_id = COALESCE",
			Payload: BulkChangePayload{MatterIDs: []string{tMatter, tMatter2}, Change: BulkChange{ResponsibleLawyerID: tContact}}},
		{Kind: GateRenumber, Effect: applyRenumber, Writes: "legacy_number = COALESCE", TwoApprovers: true,
			Payload: RenumberPayload{MatterIDs: []string{tMatter, tMatter2}, Scheme: "{yyyy}-{seq:4}",
				Numbers: map[string]string{tMatter: "2026-0001", tMatter2: "2026-0002"}}},
		{Kind: GateWidenAccess, Effect: applyWidenAccess, Writes: "visibility = $2",
			Payload: WidenAccessPayload{MatterID: tMatter, Version: 3, Restricted: &yes}},
		{Kind: GateWidenAccess, Effect: applyWidenAccess, Writes: "INSERT INTO matter.matter_member",
			Payload: WidenAccessPayload{MatterID: tMatter, Version: 3, Team: []AccessMember{{UserID: tContact, Role: "member"}}}},
		{Kind: GateWidenAccess, Effect: applyWidenAccess, Writes: "visibility = $2",
			Payload: WidenAccessPayload{MatterID: tMatter, Visibility: "team", GrantUserIDs: []string{tContact}}},
		{Kind: GateApplyTemplate, Effect: applyTemplate, Writes: "INSERT INTO docs.folder",
			Payload: ApplyTemplatePayload{MatterID: tMatter, TemplateID: tArea}},
	})
}

// The builders fill each payload the effects apply, and refuse one the effect could not.
func TestBuilders(t *testing.T) {
	yes := true
	tests := []struct {
		name  string
		build func() (json.RawMessage, error)
		want  string
	}{
		{"merge", func() (json.RawMessage, error) {
			return BuildContactMerge(ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "merge", SurvivorID: tContact2})
		}, `{"contact_ids":["` + tContact + `","` + tContact2 + `"],"action":"merge","survivor_id":"` + tContact2 + `"}`},
		{"merge without survivor", func() (json.RawMessage, error) {
			return BuildContactMerge(ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "merge"})
		}, ""},
		{"relate needs a kind", func() (json.RawMessage, error) {
			return BuildContactMerge(ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "relate"})
		}, ""},
		{"practice config", func() (json.RawMessage, error) {
			return BuildPracticeConfig(tArea, map[string]json.RawMessage{"default_roles": json.RawMessage(`["client"]`)})
		}, `{"practice_area":"` + tArea + `","change":{"default_roles":["client"]}}`},
		{"practice config unknown field", func() (json.RawMessage, error) {
			return BuildPracticeConfig(tArea, map[string]json.RawMessage{"name": json.RawMessage(`"x"`)})
		}, ""},
		{"bulk change", func() (json.RawMessage, error) {
			return BuildBulkChange([]string{tMatter}, BulkChange{RetentionClass: "RC-FILE-10"})
		}, `{"matter_ids":["` + tMatter + `"],"change":{"retention_class":"RC-FILE-10"}}`},
		{"bulk change empty", func() (json.RawMessage, error) { return BuildBulkChange([]string{tMatter}, BulkChange{}) }, ""},
		{"renumber", func() (json.RawMessage, error) {
			return BuildRenumber([]string{tMatter, tMatter2}, "{yyyy}-{seq:4}", 2026, 7)
		}, `{"matter_ids":["` + tMatter + `","` + tMatter2 + `"],"scheme":"{yyyy}-{seq:4}","numbers":{"` + tMatter + `":"2026-0007","` + tMatter2 + `":"2026-0008"}}`},
		{"renumber without seq", func() (json.RawMessage, error) { return BuildRenumber([]string{tMatter}, "{yyyy}", 2026, 1) }, ""},
		{"renumber duplicate matter", func() (json.RawMessage, error) {
			return BuildRenumber([]string{tMatter, tMatter}, "M{seq}", 2026, 1)
		}, ""},
		{"widen access route form", func() (json.RawMessage, error) {
			return BuildWidenAccess(WidenAccessPayload{MatterID: tMatter, Version: 2, Restricted: &yes})
		}, `{"matter_id":"` + tMatter + `","version":2,"restricted":true}`},
		{"widen access no change", func() (json.RawMessage, error) { return BuildWidenAccess(WidenAccessPayload{MatterID: tMatter}) }, ""},
		{"apply template", func() (json.RawMessage, error) { return BuildApplyTemplate(tMatter, tArea) },
			`{"matter_id":"` + tMatter + `","template_id":"` + tArea + `"}`},
		{"apply template no template", func() (json.RawMessage, error) { return BuildApplyTemplate(tMatter, "") }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.build()
			if tt.want == "" {
				if !errors.Is(err, gates.ErrNotApplied) {
					t.Fatalf("err = %v, want ErrNotApplied", err)
				}
				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}
