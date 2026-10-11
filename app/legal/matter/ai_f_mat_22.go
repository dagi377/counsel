package matter

import (
	"embed"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureSuggestCleanup is the spec id of "Suggest cleanup".
const FeatureSuggestCleanup = "F-MAT-22"

// suggestCleanupTrigger is how features.json words F-MAT-22's start: a screen action only.
const suggestCleanupTrigger = "on request: contact-field-settings > Suggest cleanup"

//go:embed prompts/f-mat-22.md
var promptFMAT22 embed.FS

// SuggestCleanupDef is F-MAT-22: on the contact-field-settings screen the agent ranks
// near-duplicate tags and unused fields by embedding and flags them, each with a citation. It
// is read-only: no write tool, no gate, and the ranked list goes back to the screen-action
// caller only, never stored (AI-R-005). A person makes any change.
func SuggestCleanupDef() feature.Def {
	return feature.Def{
		ID:      FeatureSuggestCleanup,
		Area:    "matters",
		Name:    "Suggest cleanup",
		Source:  feature.SourceScreenAction,
		Trigger: suggestCleanupTrigger,
		// The screen action is the trigger itself: "contact-field-settings > Suggest cleanup".
		Actions: []string{strings.TrimPrefix(suggestCleanupTrigger, "on request: ")},
		Kind:    ai.KindEmbedding,
		Tools:   []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT22,
		PromptPath: "prompts/f-mat-22.md",
		Fixture:    "testdata/ai/f-mat-22/suggest-cleanup",
	}
}
