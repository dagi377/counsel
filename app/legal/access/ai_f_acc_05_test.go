package access_test

import "testing"

// F-ACC-05 Anomaly watch (AI-016): the landed fixtures run through agenttest.FromFeature on
// the app's real handlers. A batch of access events runs a read-only chat that may alert the
// owners; it opens no gate and calls no write tool.

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestAI_F_ACC_05_AnomalyWatch(t *testing.T) {
	acccLanded(t, acc05Flow())
}
