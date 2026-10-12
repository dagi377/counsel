package matter

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/kernel/registry"
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

// kindSchema compiles kind's payload schema from spec/gates.json, the schema the agent
// payload builder registry (agent.PayloadBuilderSet) checks every payload against.
func kindSchema(t *testing.T, kind string) *jsonschema.Schema {
	t.Helper()
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if k.Code != kind {
			continue
		}
		c := jsonschema.NewCompiler()
		c.AssertFormat = true
		if err := c.AddResource("mem://"+kind+".json", bytes.NewReader(k.PayloadSchema)); err != nil {
			t.Fatal(err)
		}
		s, err := c.Compile("mem://" + kind + ".json")
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	t.Fatalf("no %s kind in spec/gates.json", kind)
	return nil
}

// Payloads are built by the agent payload builder registry (agent.PayloadBuilderSet), which
// fills them from the subject's record and checks the kind's schema; BuildRenumber is the
// one matter builder left, since it computes the numbers. Each payload the effects apply
// passes its kind's schema and the effect's own check, and one the effect could not apply
// is refused by that check.
func TestBuilders(t *testing.T) {
	yes := true
	approved := func(kind string) registry.Approved { return registry.Approved{Kind: kind} }
	renumber := func(ids []string, scheme string, first int) func() (any, error) {
		return func() (any, error) {
			raw, err := BuildRenumber(ids, scheme, 2026, first)
			return raw, err
		}
	}
	checked := func(p any, check func() error) func() (any, error) {
		return func() (any, error) { return p, check() }
	}
	merge := ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "merge", SurvivorID: tContact2}
	mergeNoSurvivor := ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "merge"}
	relateNoKind := ContactMergePayload{ContactIDs: []string{tContact, tContact2}, Action: "relate"}
	config := PracticeConfigPayload{PracticeArea: tArea, Change: map[string]json.RawMessage{"default_roles": json.RawMessage(`["client"]`)}}
	configUnknown := PracticeConfigPayload{PracticeArea: tArea, Change: map[string]json.RawMessage{"name": json.RawMessage(`"x"`)}}
	bulk := BulkChangePayload{MatterIDs: []string{tMatter}, Change: BulkChange{RetentionClass: "RC-FILE-10"}}
	bulkEmpty := BulkChangePayload{MatterIDs: []string{tMatter}}
	widen := WidenAccessPayload{MatterID: tMatter, Version: 2, Restricted: &yes, Visibility: "team"}
	widenNone := WidenAccessPayload{MatterID: tMatter}
	template := ApplyTemplatePayload{MatterID: tMatter, TemplateID: tArea}
	templateNone := ApplyTemplatePayload{MatterID: tMatter}
	planErr := func(p WidenAccessPayload) func() error {
		return func() error { _, _, err := p.plan(approved(GateWidenAccess)); return err }
	}
	bulkErr := func(p BulkChangePayload) func() error {
		return func() error { _, err := p.check(approved(GateBulkChange)); return err }
	}
	templateErr := func(p ApplyTemplatePayload) func() error {
		return func() error {
			return gates.Need(approved(GateApplyTemplate), map[string]string{"matter_id": p.MatterID, "template_id": p.TemplateID})
		}
	}
	tests := []struct {
		name  string
		kind  string
		build func() (any, error)
		want  string
	}{
		{"merge", GateContactMerge, checked(merge, func() error { return merge.check(approved(GateContactMerge)) }),
			`{"contact_ids":["` + tContact + `","` + tContact2 + `"],"action":"merge","survivor_id":"` + tContact2 + `"}`},
		{"merge without survivor", GateContactMerge, checked(mergeNoSurvivor, func() error { return mergeNoSurvivor.check(approved(GateContactMerge)) }), ""},
		{"relate needs a kind", GateContactMerge, checked(relateNoKind, func() error { return relateNoKind.check(approved(GateContactMerge)) }), ""},
		{"practice config", GatePracticeConfig, checked(config, func() error { return config.check(approved(GatePracticeConfig)) }),
			`{"practice_area":"` + tArea + `","change":{"default_roles":["client"]}}`},
		{"practice config unknown field", GatePracticeConfig, checked(configUnknown, func() error { return configUnknown.check(approved(GatePracticeConfig)) }), ""},
		{"bulk change", GateBulkChange, checked(bulk, bulkErr(bulk)),
			`{"matter_ids":["` + tMatter + `"],"change":{"retention_class":"RC-FILE-10"}}`},
		{"bulk change empty", GateBulkChange, checked(bulkEmpty, bulkErr(bulkEmpty)), ""},
		{"renumber", GateRenumber, renumber([]string{tMatter, tMatter2}, "{yyyy}-{seq:4}", 7),
			`{"matter_ids":["` + tMatter + `","` + tMatter2 + `"],"scheme":"{yyyy}-{seq:4}","numbers":{"` + tMatter + `":"2026-0007","` + tMatter2 + `":"2026-0008"}}`},
		{"renumber without seq", GateRenumber, renumber([]string{tMatter}, "{yyyy}", 1), ""},
		{"renumber duplicate matter", GateRenumber, renumber([]string{tMatter, tMatter}, "M{seq}", 1), ""},
		{"widen access route form", GateWidenAccess, checked(widen, planErr(widen)),
			`{"matter_id":"` + tMatter + `","version":2,"restricted":true,"visibility":"team"}`},
		{"widen access no change", GateWidenAccess, checked(widenNone, planErr(widenNone)), ""},
		{"apply template", GateApplyTemplate, checked(template, templateErr(template)),
			`{"matter_id":"` + tMatter + `","template_id":"` + tArea + `"}`},
		{"apply template no template", GateApplyTemplate, checked(templateNone, templateErr(templateNone)), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := tt.build()
			if tt.want == "" {
				if !errors.Is(err, gates.ErrNotApplied) {
					t.Fatalf("err = %v, want ErrNotApplied", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, ok := p.(json.RawMessage)
			if !ok {
				if got, err = json.Marshal(p); err != nil {
					t.Fatal(err)
				}
			}
			if string(got) != tt.want {
				t.Fatalf("got %s; want %s", got, tt.want)
			}
			var v any
			if err := json.Unmarshal(got, &v); err != nil {
				t.Fatal(err)
			}
			if err := kindSchema(t, tt.kind).Validate(v); err != nil {
				t.Fatalf("payload fails the %s schema: %v", tt.kind, err)
			}
		})
	}
}

// A route team change carries the visibility the matter keeps; the effect adds the members
// and never rewrites visibility, so a restricted matter is not loosened to team_only.
func TestWidenAccessTeamKeepsVisibility(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name    string
		p       WidenAccessPayload
		wantVis string
	}{
		{"route team change", WidenAccessPayload{MatterID: tMatter, Version: 3, Visibility: "team", Team: []AccessMember{{UserID: tContact}}}, ""},
		{"route restrict", WidenAccessPayload{MatterID: tMatter, Version: 3, Visibility: "team", Restricted: &yes}, "restricted"},
		{"route open", WidenAccessPayload{MatterID: tMatter, Version: 3, Visibility: "firm", Restricted: &no}, "firm_open"},
		{"spec form", WidenAccessPayload{MatterID: tMatter, Visibility: "team", GrantUserIDs: []string{tContact}}, "team_only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vis, _, err := tt.p.plan(registry.Approved{Kind: GateWidenAccess})
			if err != nil || vis != tt.wantVis {
				t.Fatalf("plan visibility = %q, %v; want %q", vis, err, tt.wantVis)
			}
		})
	}
}
