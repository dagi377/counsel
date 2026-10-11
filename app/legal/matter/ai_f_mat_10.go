package matter

import (
	"embed"
	"slices"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureRelatedMatters is the spec id of "Related matter finder".
const FeatureRelatedMatters = "F-MAT-10"

// relatedMatterEvents are the kernel event types of "party added or matter opened". The
// a2 events have no party event of their own: a party added to a matter is a
// matter.updated.
var relatedMatterEvents = []string{"matter.updated", "matter.opened"}

//go:embed prompts/f-mat-10.md
var promptFMAT10 embed.FS

// RelatedMattersDef is F-MAT-10: when a matter opens or gains a party, the agent ranks the
// firm's other matters by how closely they relate to it (embedding similarity of parties and
// documents) and proposes links on matter-overview. It is read-only: no write tool and no
// gate; a proposed link is a cited answer the user may ignore.
func RelatedMattersDef() feature.Def {
	return feature.Def{
		ID:      FeatureRelatedMatters,
		Area:    "matters",
		Name:    "Related matter finder",
		Source:  feature.SourceJob,
		Trigger: "event: party added or matter opened",
		// Related matters are ranked by embedding similarity.
		Kind:       ai.KindEmbedding,
		Tools:      []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT10,
		PromptPath: "prompts/f-mat-10.md",
		Fixture:    "testdata/ai/f-mat-10/related-matter-finder",
	}
}

// RelatedMattersBinding binds F-MAT-10 to a matter opened or updated. Event runs act for the
// event's user, so it needs no scheduled acting user.
func RelatedMattersBinding() (trigger.Binding, error) {
	return trigger.Bind(RelatedMattersDef(), "", slices.Clone(relatedMatterEvents)...)
}
