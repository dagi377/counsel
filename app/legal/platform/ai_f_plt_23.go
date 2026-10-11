package platform

import (
	"embed"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionDescribeReport is the custom-report-builder screen action that runs F-PLT-23.
const ActionDescribeReport = "custom-report-builder > Describe report in words"

//go:embed prompts/f-plt-23.md
var promptFPLT23 embed.FS

// DescribeReportDef is F-PLT-23: on the custom-report-builder screen a user describes a report
// in a sentence. The agent turns it into a dataset, columns and filters for the user to check;
// the user builds the report. It is read-only: no write tool, no gate, and the answer goes
// back to the screen-action caller only, never stored (AI-R-005).
func DescribeReportDef() feature.Def {
	return feature.Def{
		ID:      "F-PLT-23",
		Area:    "platform",
		Name:    "Describe report in words",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionDescribeReport,
		Actions: []string{ActionDescribeReport},
		Kind:    ai.KindChat,
		Tools:   []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument},
		// The run's agent principal holds the capabilities of its read tools and no other.
		Grant:      []string{agent.CapMattersView, agent.CapDocumentsView},
		Prompt:     promptFPLT23,
		PromptPath: "prompts/f-plt-23.md",
		Fixture:    "testdata/ai/f-plt-23/describe-report-in-words",
	}
}
