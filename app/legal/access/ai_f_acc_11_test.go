package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// F-ACC-11 Explain pattern (AI-022): on access-audit > Explain pattern the agent summarizes
// what is unusual about the selected events from the matters it can see. Read-only: no gate
// row, no write tool call. The fixtures run through agenttest.FromFeature on the app's real
// search_matters handler, with no stand-ins.

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestAI_R_002_ACC11_ExplainPattern(t *testing.T) {
	accdLanded(t, access.ExplainPatternDef(), "screen: access-audit\nrecords: firm:firm-synthetic", "2026-0711", "2026-0712")
}
