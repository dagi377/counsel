package access

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
)

// FeatureOffboardingPlan is the spec id of "Offboarding plan".
const FeatureOffboardingPlan = "F-ACC-06"

// GateOffboarding is gate GA12: an admin with admin.offboard approves a user's offboarding
// and its handover. No module registers it yet; F-ACC-06 opens it as a tool-gap kind.
const GateOffboarding = "core.offboarding"

// EventOffboardStarted is published when an admin starts offboarding a user. a2 names no
// event for it (docs/QUESTIONS.md Q-FEAT-ACC-04).
const EventOffboardStarted = "user.offboard_started"

// ActionProposeSuccessors is the offboard-user screen action that runs F-ACC-06 on request.
const ActionProposeSuccessors = "offboard-user > Propose successors"

//go:embed prompts/f-acc-06.md
var promptFACC06 embed.FS

// OffboardingPlanDef is F-ACC-06: when a user's offboarding starts, the agent finds the
// matters the leaver holds, drafts the handover with draft_document and submits the draft for
// gate GA12 (core.offboarding) with submit_for_approval. The gate's preparer is the acting
// user; an admin other than the preparer decides it and the agent never does.
func OffboardingPlanDef() feature.Def {
	return feature.Def{
		ID:      FeatureOffboardingPlan,
		Area:    "access",
		Name:    "Offboarding plan",
		Source:  feature.SourceJob,
		Trigger: "event (Offboard started)",
		Actions: []string{ActionProposeSuccessors},
		Tools:   []string{"search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval"},
		// rule: ARC-I-035
		// core.offboarding has no dedicated write tool: the run drafts the handover and submits it.
		GateKinds: []string{GateOffboarding},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "documents.edit", "matters.view"},
		Prompt:     promptFACC06,
		PromptPath: "prompts/f-acc-06.md",
		Fixture:    "testdata/ai/f-acc-06/offboarding-plan",
	}
}

// OffboardingPlanBinding binds F-ACC-06 to the offboard-started event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func OffboardingPlanBinding() (trigger.Binding, error) {
	return trigger.Bind(OffboardingPlanDef(), "", EventOffboardStarted)
}
