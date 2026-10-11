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
		{"F-DL-07", true},
		{"F-DL-05", false},
		{"F-DL-10", false},
		{"F-DL-11", false},
		{"F-DL-12", false},
		{"F-DL-13", false},
		{"F-DL-08", false},
		{"F-DL-09", true},
		{"F-CON-02", false},
		{"F-CON-03", true},
		{"F-CON-04", true},
		{"F-CON-05", false},
		{"F-CON-06", true},
		{"F-CON-07", false},
		{"F-CON-08", true},
		{"F-CON-09", true},
		{"F-CON-10", true},
		{"F-CON-15", true},
		{"F-CON-16", true},
		{"F-DOC-02", true},
		{"F-DOC-03", true},
		{"F-TIM-01", true},
		{"F-DOC-04", true},
		{"F-DOC-07", true},
		{"F-DOC-08", false},
		{"F-DOC-09", true},
		{"F-DOC-10", true},
		{"F-DOC-11", true},
		{"F-DOC-15", false},
		{"F-DOC-16", true},
		{"F-DOC-17", false},
		{"F-DOC-19", true},
		{"F-DOC-05", false},
		{"F-DOC-12", false},
		{"F-DOC-14", false},
		{"F-DOC-20", false},
		{"F-DOC-21", false},
		{"F-DOC-22", false},
		{"F-MAT-19", true},
		{"F-MAT-02", false},
		{"F-MAT-10", false},
		{"F-MAT-15", false},
		{"F-MAT-20", false},
		{"F-MAT-22", false},
		{"F-MAT-23", false},
		{"F-INT-01", false},
		{"F-INT-10", true},
		{"F-INT-12", true},
		{"F-INT-14", true},
		{"F-INT-07", false},
		{"F-INT-09", false},
		{"F-INT-20", false},
		{"F-TIM-05", false},
		{"F-TIM-06", true},
		{"F-TIM-07", true},
		{"F-TIM-10", false},
		{"F-TIM-08", false},
		{"F-TIM-09", false},
		{"F-TIM-11", false},
		{"F-TIM-12", false},
		{"F-WRK-01", true},
		{"F-WRK-02", true},
		{"F-WRK-04", true},
		{"F-WRK-06", true},
		{"F-WRK-07", true},
		{"F-WRK-09", true},
		{"F-WRK-11", false},
		{"F-WRK-14", true},
		{"F-WRK-05", false},
		{"F-WRK-08", false},
		{"F-WRK-15", false},
		{"F-WRK-16", false},
		{"F-WRK-17", false},
		{"F-PLT-04", true},
		{"F-PLT-13", false},
		{"F-PLT-17", false},
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
