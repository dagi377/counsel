package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-13.md
var promptFPLT13 embed.FS

// FeatureFilingScopeReevaluation is the spec id of "Filing scope re-evaluation".
const FeatureFilingScopeReevaluation = "F-PLT-13"

// EventSyncScopeChanged is the kernel event a change to the firm's mail sync scope
// (email-settings sync_scope: contacts, contacts_and_staff or everyone) emits, before the
// change applies. Its user is the person who asked for the change. It starts F-PLT-13.
const EventSyncScopeChanged = "email.sync_scope_changed"

// FilingScopeReevaluationDef is F-PLT-13: when the sync scope changes, the agent reads the
// matters and the mail filed to them and answers with a preview diff of the items the new
// scope would file or unfile, each with a citation, for the person to confirm on
// email-settings. It is read-only: no write tool and no gate kind; the change applies only
// after that person confirms it, and the agent files or unfiles nothing.
func FilingScopeReevaluationDef() feature.Def {
	return feature.Def{
		ID:      FeatureFilingScopeReevaluation,
		Area:    "platform",
		Name:    "Filing scope re-evaluation",
		Source:  feature.SourceJob,
		Trigger: "event: sync scope changed",
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// rule: AI-R-003
		// A read-only feature: no write tool, no gate kind, and the grant of its reads only.
		Grant:      []string{agent.CapDocumentsView, agent.CapMattersView},
		Prompt:     promptFPLT13,
		PromptPath: "prompts/f-plt-13.md",
		Fixture:    "testdata/ai/f-plt-13/filing-scope-re-evaluation",
	}
}

// FilingScopeReevaluationBinding binds F-PLT-13 to the sync-scope change. An event run acts
// for the event's user, the person who asked for the change and confirms the preview.
func FilingScopeReevaluationBinding() (trigger.Binding, error) {
	return trigger.Bind(FilingScopeReevaluationDef(), "", EventSyncScopeChanged)
}
