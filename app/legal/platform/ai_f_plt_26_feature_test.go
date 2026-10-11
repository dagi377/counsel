package platform_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/platform"
)

// rule: AI-R-003
func TestFPLT26_DefMatchesSpec(t *testing.T) {
	pltfCheckDefMatchesSpec(t, platform.SuggestReplyDef())
}

// rule: AI-R-005
func TestFPLT26_Prompt(t *testing.T) {
	pltjCheckPrompt(t, platform.SuggestReplyDef())
}

// F-PLT-26 runs only from its screen action, mail-add-in > Suggest reply.
func TestFPLT26_TriggerBinding(t *testing.T) {
	pltjCheckScreenAction(t, platform.SuggestReplyDef(), "mail-add-in > Suggest reply")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT26_FromFeature(t *testing.T) {
	pltjCheckReadOnly(t, platform.SuggestReplyDef(), pltjReadOnlyCase{
		action: platform.ActionSuggestReply,
		lines: []string{
			"Client email asking when the closing documents will be ready.",
			"Closing documents are due to the client on 2026-10-01.",
		},
		answer: "Thank you for your note. The closing documents are on track and we will send them by the due date.",
	})
}

// rule: ARC-I-038
// rule: ARC-I-039
func TestFPLT26_AgentNeverApproves(t *testing.T) {
	pltjCheckNoApproval(t, platform.SuggestReplyDef())
}
