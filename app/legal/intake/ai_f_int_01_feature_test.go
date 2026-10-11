package intake_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// fint01Card is the synthetic triage card the extraction produced for a lead.
func fint01Card() intake.TriageCard {
	cite := func(from, to int) agenttools.Citation {
		return agenttools.Citation{VersionID: "lead-form-v1", Page: 1, FromLine: from, ToLine: to}
	}
	return intake.TriageCard{LeadID: "00000000-0000-4000-8000-0000000001ea", Items: []intake.TriageItem{
		{Kind: intake.TriageSummary, Text: "Supplier stopped delivery under a signed contract.", Citation: cite(1, 2)},
		{Kind: intake.TriagePracticeArea, Text: "Commercial litigation.", Citation: cite(1, 1)},
		{Kind: intake.TriageMissingInfo, Text: "Date the contract was signed.", Citation: cite(3, 3)},
	}}
}

// rule: AI-R-003
// rule: ARC-I-035
// The write path drafts every cited line of the card and opens only G1 on it; a card that
// would draft an uncited or empty line drafts nothing.
func TestFINT01_WriteInputs(t *testing.T) {
	const matterID = "00000000-0000-4000-8000-0000000000a1"
	tests := []struct {
		name     string
		matterID string
		edit     func(*intake.TriageCard)
		wantErr  error
	}{
		{"cited card", matterID, func(*intake.TriageCard) {}, nil},
		{"no matter", "", func(*intake.TriageCard) {}, intake.ErrBadTriageItem},
		{"no lead", matterID, func(c *intake.TriageCard) { c.LeadID = "" }, intake.ErrBadTriageItem},
		{"no items", matterID, func(c *intake.TriageCard) { c.Items = nil }, intake.ErrBadTriageItem},
		{"empty line", matterID, func(c *intake.TriageCard) { c.Items[1].Text = "  " }, intake.ErrBadTriageItem},
		{"uncited line", matterID, func(c *intake.TriageCard) { c.Items[2].Citation = agenttools.Citation{} }, agenttools.ErrCitation},
		{"bad line range", matterID, func(c *intake.TriageCard) { c.Items[0].Citation.ToLine = 0 }, agenttools.ErrCitation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := fint01Card()
			tt.edit(&card)
			raw, err := intake.TriageDraftInput(tt.matterID, card)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) || !errors.Is(err, intake.ErrBadTriageItem) || raw != nil {
					t.Fatalf("draft input = %s, %v; want nothing and %v", raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var in struct {
				MatterID string `json:"matter_id"`
				Title    string `json:"title"`
				Body     string `json:"body_markdown"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				t.Fatal(err)
			}
			if in.MatterID != tt.matterID || in.Title == "" {
				t.Fatalf("draft input = %+v", in)
			}
			for _, it := range card.Items {
				if !strings.Contains(in.Body, it.Text) || !strings.Contains(in.Body, it.Citation.VersionID) {
					t.Fatalf("body %q lacks the cited line %q", in.Body, it.Text)
				}
			}
		})
	}
	sub, err := intake.TriageSubmitInput(intbDraftID(1))
	if err != nil || !strings.Contains(string(sub), `"gate_kind":"`+intake.GateLeadDecision+`"`) {
		t.Fatalf("submit input = %s, %v; want gate kind %s", sub, err, intake.GateLeadDecision)
	}
	if _, err := intake.TriageSubmitInput(""); !errors.Is(err, intake.ErrBadTriageItem) {
		t.Fatalf("submit input with no draft = %v, want ErrBadTriageItem", err)
	}
}

// fint01Run seeds the intake matter and a known contact named after short, then runs F-INT-01
// with a script that drafts the triage card through the write path and submits it under
// gateKind.
func fint01Run(t *testing.T, short, gateKind string) (*agenttest.FeatureDriver, *intbDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := intbFeature(t, intake.TriageNewLeadDef())
	m := fd.Seed.Matter("2026-0102", short)
	draft, err := intake.TriageDraftInput(string(m.ID), fint01Card())
	if err != nil {
		t.Fatal(err)
	}
	submit, err := intake.TriageSubmitInput(intbDraftID(1))
	if err != nil {
		t.Fatal(err)
	}
	if gateKind != intake.GateLeadDecision {
		submit = json.RawMessage(strings.Replace(string(submit), intake.GateLeadDecision, gateKind, 1))
	}
	calls := intbCalls("Synth Lead", string(m.ID), "", "", gateKind)
	calls[3] = intbCall{agent.ToolDraftDocument, string(draft)}
	calls[4] = intbCall{agent.ToolSubmitForApproval, string(submit)}
	model := intbScript("F-INT-01", calls, "Drafted the triage card for G1.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint01", "event: lead.created"), intbToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFINT01_FromFeature(t *testing.T) {
	fd, drafts, run, _ := fint01Run(t, "Synth Lead", intake.GateLeadDecision)
	intbCheckRun(t, fd, drafts, run, intake.GateLeadDecision)
}

// rule: ARC-I-035
// intake.lead_decision is a tool-gap kind: draft_document then submit_for_approval opens it,
// and submit_for_approval may open no kind outside the Def.
func TestFINT01_GateKindNotAllowed(t *testing.T) {
	for _, kind := range []string{intbSkillAutonomy, "conflict.clearance", "matter.opening", "trust.withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			_, drafts, run, _ := fint01Run(t, "Synth Lead", kind)
			intbCheckKindRefused(t, drafts, run)
		})
	}
}

// rule: ARC-I-038
func TestFINT01_Injection(t *testing.T) {
	fd, _, run, want := fint01Run(t, "Synth Lead "+intbInjected, intake.GateLeadDecision)
	intbCheckInjection(t, fd, run, want, intbInjected)
}
