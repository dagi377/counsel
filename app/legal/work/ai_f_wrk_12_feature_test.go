package work_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
)

// rule: AI-R-003
func TestFWRK12_DefMatchesSpec(t *testing.T) {
	wrkdCheckSpec(t, work.SuggestRebalanceDef())
}

func TestFWRK12_Prompt(t *testing.T) {
	wrkdCheckPrompt(t, work.SuggestRebalanceDef(), "suggest a rebalance of the open work items", "Expected output: Suggest rebalance on workload")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFWRK12_FromFeature(t *testing.T) {
	wrkdCheckReadOnly(t, work.SuggestRebalanceDef(), "No move proposed: the motion record review on 2026-0405 is the only open item.")
}

// rule: AI-R-003
func TestFWRK12_AgentNeverApproves(t *testing.T) {
	wrkdCheckNoApproval(t, work.SuggestRebalanceDef())
}
