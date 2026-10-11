package matter

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

// FeatureRequestID is the spec id of "Request ID".
const FeatureRequestID = "F-MAT-19"

// GateClientID is the gate kind of spec gate G4: a person verifies the client's identity.
// It is the same kind the conflicts features open (conflict.GateClientID).
const GateClientID = "conflict.client_id"

// requestIDTrigger is how features.json words F-MAT-19's start: a screen action only.
const requestIDTrigger = "on request: contact-detail > Request ID"

//go:embed prompts/f-mat-19.md
var promptFMAT19 embed.FS

// RequestIDDef is F-MAT-19: from a contact's page the agent drafts the portal request that
// asks the client for identification, with draft_document, and opens gate
// conflict.client_id (G4), a tool-gap kind, on it with submit_for_approval. The gate is
// prepared by the acting user; a person other than them decides it.
func RequestIDDef() feature.Def {
	return feature.Def{
		ID:      FeatureRequestID,
		Area:    "matters",
		Name:    "Request ID",
		Source:  feature.SourceScreenAction,
		Trigger: requestIDTrigger,
		// The screen action is the trigger itself: "contact-detail > Request ID".
		Actions: []string{strings.TrimPrefix(requestIDTrigger, "on request: ")},
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts", "draft_document",
			"report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateClientID},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFMAT19,
		PromptPath: "prompts/f-mat-19.md",
		Fixture:    "testdata/ai/f-mat-19/request-id",
	}
}
