package access_test

import "testing"

// F-ACC-09 Explain access (AI-020): the landed fixtures run through agenttest.FromFeature on the app's
// real handlers. Read-only: no gate row and no write tool.

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestAI_R_002_ACC09_ExplainAccess(t *testing.T) {
	acccLanded(t, acc09Flow())
}
