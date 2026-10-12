package access_test

import "testing"

// F-ACC-08 Explain role (AI-019): the landed fixtures run through agenttest.FromFeature on the app's
// real handlers. Read-only: no gate row and no write tool.

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestAI_R_002_ACC08_ExplainRole(t *testing.T) {
	acccLanded(t, acc08Flow())
}
