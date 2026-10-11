package matter

import (
	"embed"
	"slices"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureMatterDigest is the spec id of "Matter digest and risk score".
const FeatureMatterDigest = "F-MAT-02"

// digestEvents are the kernel event types of "any matter event": every event the matter
// module publishes (spec/eng/a2-interfaces.json events).
var digestEvents = []string{"matter.opened", "matter.updated", "matter.stage_changed", "matter.team_changed", "matter.closed"}

//go:embed prompts/f-mat-02.md
var promptFMAT02 embed.FS

// MatterDigestDef is F-MAT-02: nightly, on any matter event and on request from firm home,
// the agent reads the firm's matters and writes the digest and risk score that firm home and
// the matter list show. It is read-only: no write tool and no gate; the cited answer is the
// output.
func MatterDigestDef() feature.Def {
	return feature.Def{
		ID:         FeatureMatterDigest,
		Area:       "matters",
		Name:       "Matter digest and risk score",
		Source:     feature.SourceJob,
		Trigger:    "schedule: nightly, and on any matter event",
		Actions:    []string{"firm-home > Compose morning digest"},
		Kind:       ai.KindChat,
		Tools:      []string{"get_matter", "search_documents", "read_document", "search_contacts"},
		Grant:      []string{"contacts.view", "documents.view", "matters.view"},
		Prompt:     promptFMAT02,
		PromptPath: "prompts/f-mat-02.md",
		Fixture:    "testdata/ai/f-mat-02/matter-digest-and-risk-score",
	}
}

// MatterDigestBinding binds F-MAT-02 to its nightly schedule and to every matter event. The
// nightly run acts for the firm's designated lawyer for F-MAT-02 and does not start while
// none is set; an event run acts for the event's user. The on-request start is the Def's
// screen action, "firm-home > Compose morning digest", served by trigger.Actions.
func MatterDigestBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(MatterDigestDef(), slices.Clone(digestEvents)...)
}
