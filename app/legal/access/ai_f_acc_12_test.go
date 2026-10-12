package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// F-ACC-12 Explain change (AI-023): on sign-in-settings > Explain change the agent states who
// the draft sign-in policy would lock out or force to re-enrol. Read-only: no gate
// row, no write tool call. The fixtures run through agenttest.FromFeature on the app's real
// search_matters handler, with no stand-ins.

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestAI_R_002_ACC12_ExplainChange(t *testing.T) {
	accdLanded(t, access.ExplainChangeDef(), "screen: sign-in-settings\nrecords: firm:firm-synthetic", "2026-0711", "2026-0712")
}
