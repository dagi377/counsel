package feature_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/featureset"
)

// The app's feature Set comes from featureset, which imports package feature, so these checks
// live in the external test package and call the gate in spec_test.go.

// rule: AI-R-002
// Every Def boot registers matches spec/ai/features.json, and every spec feature has a Def
// (strictSync).
func TestAI_R_002_RegisteredDefsMatchSpec(t *testing.T) {
	s, err := featureset.App()
	if err != nil {
		t.Fatalf("featureset.App: %v", err)
	}
	feature.CheckRegistered(t, s)
}

// rule: AI-R-002
// Every spec feature runs through agenttest.FromFeature on its real tool handlers: the app's
// Def when one is registered, else a Def built from its spec entry, so a feature still on an
// unmerged branch is held to the same check.
func TestAI_R_002_FromFeatureHasNoStandIns(t *testing.T) {
	s, err := featureset.App()
	if err != nil {
		t.Fatalf("featureset.App: %v", err)
	}
	standIns := map[string][]string{}
	for _, id := range feature.SpecIDs(t) {
		d, ok := s.ByID(id)
		if !ok {
			d = feature.SpecDef(t, id)
		}
		if fd := agenttest.FromFeature(t, d); len(fd.StandIns) > 0 {
			standIns[id] = fd.StandIns
		}
	}
	feature.CheckStandIns(t, standIns)
}
