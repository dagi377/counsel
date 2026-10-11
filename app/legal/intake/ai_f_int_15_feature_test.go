package intake_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/intake"
)

// fint15Blocked names why the F-INT-15 runs cannot pass yet: draft_note opens only
// matter.agent_note, outside the run scope, and the G3 payload is a lead decision, not a note
// (Q-FEAT-INT-11 in docs/QUESTIONS.md). The tests are complete; drop the skip once draft_note
// can open intake.consult_outcome.
const fint15Blocked = "blocked on Q-FEAT-INT-11: draft_note cannot open intake.consult_outcome"

// rule: AI-R-003
// rule: ARC-I-035
// F-INT-15 runs on request only, from consult-outcome > Generate non-engagement letter.
func TestFINT15_DefMatchesSpec(t *testing.T) {
	inteCheckDefMatchesSpec(t, intake.NonEngagementLetterDef(), intake.ActionNonEngagementLetter)
}

// rule: AI-R-005
func TestFINT15_Prompt(t *testing.T) {
	inteCheckPrompt(t, intake.NonEngagementLetterDef(),
		"Generate non-engagement letter",
		"Generate non-engagement letter on consult-outcome (States what was and was not done and mentions limitation risk)",
		inteConsultOutcome, "report_plan", "draft_note", "limitation risk")
}

// fint15Run seeds the consultation matter named after short, then runs F-INT-15 with a script
// that drafts the letter with draft_note.
func fint15Run(t *testing.T, short string) (*agenttest.FeatureDriver, *inteDrafts, agenttest.FeatureRun, []string) {
	t.Helper()
	fd, drafts := inteFeature(t, intake.NonEngagementLetterDef())
	m := fd.Seed.Matter("2026-0115", short)
	calls := append(inteReads("Synth Consult"), inteCall{agent.ToolDraftNote, fmt.Sprintf(`{"matter_id": %q, "body_markdown": %q}`, string(m.ID),
		"Thank you for meeting with us. We held one consultation and will not act for you: we were not retained, gave no advice to rely on and took no steps. A limitation period may apply to your claim; get other legal advice promptly.")})
	model := inteScript("F-INT-15", calls, "Drafted the non-engagement letter for review.")
	return fd, drafts, fd.Fire(context.Background(), model, "run-fint15", intake.ActionNonEngagementLetter), inteToolNames(calls)
}

// rule: ARC-I-035
// rule: ARC-I-039
func TestFINT15_FromFeature(t *testing.T) {
	t.Skip(fint15Blocked)
	fd, drafts, run, _ := fint15Run(t, "Synth Consult")
	inteCheckRun(t, fd, run, agent.ToolDraftNote, inteConsultOutcome)
	if len(drafts.notes) != 1 || drafts.notes[0].PreparedBy != agenttest.FeatureActingFor {
		t.Fatalf("notes = %+v, want one prepared by %s", drafts.notes, agenttest.FeatureActingFor)
	}
}

// rule: ARC-I-038
func TestFINT15_Injection(t *testing.T) {
	t.Skip(fint15Blocked)
	fd, _, run, want := fint15Run(t, "Synth Consult "+inteInjected)
	inteCheckInjection(t, fd, run, want, inteInjected)
}
