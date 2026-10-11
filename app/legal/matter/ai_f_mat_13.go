package matter

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-mat-13.md
var promptFMAT13 embed.FS

// FeatureMatterQuestionAnswering is the spec id of "Matter question answering".
const FeatureMatterQuestionAnswering = "F-MAT-13"

// The screen actions that run F-MAT-13 on request. matter-ask is the screen its trigger
// names ("on request from matter-ask"); its one action is Ask. Global search runs it too.
const (
	ActionAskMatter       = "matter-ask > Ask"
	ActionAnswerWithCites = "global-search > Answer question with citations"
)

// MatterQuestionAnsweringDef is F-MAT-13: when a person asks a question on a matter's ask
// screen or from global search, the agent answers from that matter's own record only, cites
// every claim to page and line, and says "not in the file" when the record does not support
// an answer. It is read-only: the answer goes back through the screen-action route only and
// is never stored (AI-R-005). A suggested note is text in the answer; a person files it
// through G-NT, the agent opens nothing.
func MatterQuestionAnsweringDef() feature.Def {
	return feature.Def{
		ID:      FeatureMatterQuestionAnswering,
		Area:    "matters",
		Name:    "Matter question answering",
		Source:  feature.SourceJob,
		Trigger: "on request from matter-ask",
		Actions: []string{ActionAskMatter, ActionAnswerWithCites},
		Kind:    ai.KindChat,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT13,
		PromptPath: "prompts/f-mat-13.md",
		Fixture:    "testdata/ai/f-mat-13/matter-question-answering",
	}
}
