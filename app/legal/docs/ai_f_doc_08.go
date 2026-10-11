package docs

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-doc-08.md
var promptFDOC08 embed.FS

// FeatureEmailFiler is the spec id of "email-filer".
const FeatureEmailFiler = "F-DOC-08"

// EventEmailReceived is the event a connected mailbox emits when an email arrives, with the
// filed message's ids and the mailbox owner as user_id. It starts F-DOC-08.
const EventEmailReceived = "email.received"

// emailFilerGate is docs.filing_privilege (G-D1), the filing-queue review an email the run
// cannot file plainly goes to.
const emailFilerGate = "docs.filing_privilege"

// EmailFilerDef is F-DOC-08: when an email arrives in a connected mailbox the agent reads it,
// finds the matter it belongs to and drafts the filing. Where the matter is uncertain or the
// email may be privileged it submits a G-D1 review item to docs.filing_privilege, a tool-gap
// kind, with the acting user as preparer; a person with the gate capability decides it.
func EmailFilerDef() feature.Def {
	return feature.Def{
		ID:      FeatureEmailFiler,
		Area:    "documents",
		Name:    "email-filer",
		Source:  feature.SourceJob,
		Trigger: "event: email arrives in connected mailbox",
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// docs.filing_privilege has no dedicated write tool: the run drafts the review item and
		// submits it.
		GateKinds:  []string{emailFilerGate},
		Grant:      []string{"agent.ask", "documents.edit", "documents.view", "matters.view"},
		Prompt:     promptFDOC08,
		PromptPath: "prompts/f-doc-08.md",
		Fixture:    "testdata/ai/f-doc-08/email-filer",
	}
}

// EmailFilerBinding binds F-DOC-08 to an email arriving in a connected mailbox. Event runs act
// for the event's user (the mailbox owner), so it needs no scheduled acting user.
func EmailFilerBinding() (trigger.Binding, error) {
	return trigger.Bind(EmailFilerDef(), "", EventEmailReceived)
}
