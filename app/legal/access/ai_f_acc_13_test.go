package access_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/access"
)

// F-ACC-13 Summarize session (AI-024): on support-access > Summarize session the agent lists in plain
// language what support viewed or changed in the session. Read-only: no gate
// row, no write tool call. The fixtures run through agenttest.FromFeature on the app's real
// search_matters handler, with no stand-ins.

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestAI_R_002_ACC13_SummarizeSession(t *testing.T) {
	accdLanded(t, access.SummarizeSessionDef(), "screen: support-access\nrecords: firm:firm-synthetic", "2026-0711", "2026-0712")
}
