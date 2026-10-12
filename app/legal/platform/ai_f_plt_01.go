// Package platform holds Counsel's platform AI features (spec/ai/features.json area
// "platform"): portal triage, mail, replies and the other cross-matter communication
// features. Each feature file exports its feature.Def; app/legal/agent/featureset adds them
// to the feature.Set, so nothing registers from init().
package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-01.md
var promptFPLT01 embed.FS

// FeaturePortalTriage is the spec id of "Portal triage".
const FeaturePortalTriage = "F-PLT-01"

// EventClientPortalMessage is the event the client portal publishes when a client posts a
// message on one of their matters, with the matter and the responsible lawyer as user_id
// (docs/QUESTIONS.md Q-FEAT-PLT-01). It starts F-PLT-01.
const EventClientPortalMessage = "comms.portal_message_received"

// Client-portal screen actions that run F-PLT-01 on request.
const (
	ActionReplyRoutineStatus = "client-portal > Reply with routine status"
	ActionReplySubstance     = "client-portal > Reply with substance or advice"
)

// GatePortalMessage is G-PM: a person approves the wording before a message reaches the
// client through the portal (legaldocs.GatePortalMessage).
const GatePortalMessage = legaldocs.GatePortalMessage

// PortalTriageDef is F-PLT-01: when a client posts on the portal, and on request from the
// client portal, the agent reads the matter and its documents and drafts the reply with
// draft_email (never submitted for sending), then opens platform.portal_message (G-PM) on the
// draft with submit_for_approval. A person other than the acting user approves the wording;
// the agent never sends, approves or answers the client itself.
func PortalTriageDef() feature.Def {
	return feature.Def{
		ID:      FeaturePortalTriage,
		Area:    "platform",
		Name:    "Portal triage",
		Source:  feature.SourceJob,
		Trigger: "event: client portal message",
		Actions: []string{ActionReplyRoutineStatus, ActionReplySubstance},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// G-PM is the only kind this run may open: draft_email with submit (G-OUT, which
		// sends) is refused by the run scope.
		GateKinds: []string{GatePortalMessage},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFPLT01,
		PromptPath: "prompts/f-plt-01.md",
		Fixture:    "testdata/ai/f-plt-01/portal-triage",
	}
}

// PortalTriageBinding binds F-PLT-01 to a client portal message. Event runs act for the
// event's user (the matter's responsible lawyer), so it needs no scheduled acting user.
func PortalTriageBinding() (trigger.Binding, error) {
	return trigger.Bind(PortalTriageDef(), "", EventClientPortalMessage)
}
