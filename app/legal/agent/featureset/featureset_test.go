package featureset

import (
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
)

var updateSkills = flag.Bool("update-skills", false, "rewrite the plugin skills from the app's definitions")

const repoSkills = "../../../../" + feature.SkillsDir

func TestAppHoldsEveryListedDef(t *testing.T) {
	s, err := App()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id      string
		toolUse bool
	}{
		{"F-DL-01", false},
		{"F-DL-02", true},
		{"F-DL-03", true},
		{"F-DL-04", false},
		{"F-DL-06", true},
		{"F-CON-04", true},
		{"F-CON-05", false},
		{"F-CON-06", true},
		{"F-CON-07", false},
		{"F-CON-10", true},
		{"F-CON-15", true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			d, ok := s.ByID(tt.id)
			if !ok {
				t.Fatalf("App() has no %s", tt.id)
			}
			if d.ToolUse() != tt.toolUse {
				t.Fatalf("%s ToolUse() = %v, want %v", tt.id, d.ToolUse(), tt.toolUse)
			}
		})
	}
	var ids []string
	for _, d := range s.All() {
		ids = append(ids, d.ID)
	}
	if len(ids) != len(appDefs()) || slices.Contains(ids, "") {
		t.Fatalf("App() ids = %v, want one per appDefs entry", ids)
	}
}

// TestExportSkillsCheckedIn fails while the plugin's skills differ from the app's
// definitions; go generate runs it with -update-skills to rewrite them.
func TestExportSkillsCheckedIn(t *testing.T) {
	s, err := App()
	if err != nil {
		t.Fatal(err)
	}
	if *updateSkills {
		if err := feature.ExportSkills(s, repoSkills); err != nil {
			t.Fatal(err)
		}
	}
	if err := feature.CheckSkills(s, os.DirFS(repoSkills)); err != nil {
		t.Fatal(err)
	}
}
