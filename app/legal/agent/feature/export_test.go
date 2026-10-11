package feature

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/ai"
)

var updateSkills = flag.Bool("update-skills", false, "rewrite the plugin skills from the app's definitions")

const (
	repoSkills   = "../../../../" + SkillsDir
	repoManifest = "../../../../paploo/counsel-plugin/paploo.yaml"
)

func skillDef(id, name string, kind ai.CallKind) Def {
	return Def{
		ID: id, Area: "test", Name: name, Source: SourceScreenAction, Trigger: "Synthetic > Run", Kind: kind,
		Tools: []string{"report_plan", "draft_note"}, GateKinds: []string{"matter.note"},
		Prompt:     fstest.MapFS{"p.md": {Data: []byte("# " + id + " " + name + "\n\nDraft a note. It opens gate matter.note.\n")}},
		PromptPath: "p.md",
	}
}

func skillSet(t *testing.T, defs ...Def) *Set {
	t.Helper()
	s := NewSet()
	for _, d := range defs {
		if err := s.Add(d); err != nil {
			t.Fatalf("add %s: %v", d.ID, err)
		}
	}
	return s
}

// rule: ARC-I-046
func TestARC_I_046_SkillRendersTurnPrefixToolsAndPrompt(t *testing.T) {
	s := skillSet(t, skillDef("F-TST-01", "Synthetic note", ""), skillDef("F-TST-02", "Synthetic extract", ai.KindExtraction))
	skills, err := Skills(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("skills = %d, want 1 (tool use only)", len(skills))
	}
	body := string(skills["f-tst-01-synthetic-note"])
	tests := []struct {
		name string
		want string
	}{
		{"front matter name", "name: f-tst-01-synthetic-note\n"},
		{"generated mark", generatedMark},
		{"feature line of the turn prefix", "    feature: F-TST-01\n"},
		{"run line of the turn prefix", "    run: <run id>\n"},
		{"report tool", "- `report_plan` (report)\n"},
		{"write tool", "- `draft_note` (write)\n"},
		{"gate kind", "- `matter.note`\n"},
		{"system prompt starts with its preamble", "## Prompt\n\nfeature: F-TST-01\n"},
		{"shared rules", Rules()},
		{"feature prompt", "# F-TST-01 Synthetic note\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(body, tc.want) {
				t.Fatalf("skill lacks %q:\n%s", tc.want, body)
			}
		})
	}
	if got := TurnPrefix("F-TST-01", "run-1"); got != "feature: F-TST-01\nrun: run-1\n" {
		t.Fatalf("TurnPrefix = %q", got)
	}
}

func TestExportSkillsThenCheck(t *testing.T) {
	s := skillSet(t, skillDef("F-TST-01", "Synthetic note", ""))
	tests := []struct {
		name string
		mod  func(t *testing.T, dir string)
		want error
	}{
		{"fresh export matches", func(*testing.T, string) {}, nil},
		{"edited skill is stale", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "f-tst-01-synthetic-note", SkillFile)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(b, "extra\n"...), 0o644); err != nil {
				t.Fatal(err)
			}
		}, ErrSkillsStale},
		{"removed skill is stale", func(t *testing.T, dir string) {
			if err := os.RemoveAll(filepath.Join(dir, "f-tst-01-synthetic-note")); err != nil {
				t.Fatal(err)
			}
		}, ErrSkillsStale},
		{"generated skill without a definition is stale", func(t *testing.T, dir string) {
			writeSkill(t, dir, "f-old-01-gone", generatedMark+"\n")
		}, ErrSkillsStale},
		{"hand-written skill is ignored", func(t *testing.T, dir string) {
			writeSkill(t, dir, "continue-after-gate", "# hand written\n")
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := ExportSkills(s, dir); err != nil {
				t.Fatal(err)
			}
			tc.mod(t, dir)
			if err := CheckSkills(s, os.DirFS(dir)); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("CheckSkills = %v, want %v", err, tc.want)
			}
		})
	}
	// A second export removes the stale generated skill and keeps the hand-written one.
	dir := t.TempDir()
	writeSkill(t, dir, "f-old-01-gone", generatedMark+"\n")
	writeSkill(t, dir, "continue-after-gate", "# hand written\n")
	if err := ExportSkills(s, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "f-old-01-gone")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale skill kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "continue-after-gate", SkillFile)); err != nil {
		t.Fatalf("hand-written skill removed: %v", err)
	}
}

func writeSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, SkillFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExportSkillsCheckedIn fails while the plugin's skills differ from the app's
// definitions; go generate runs it with -update-skills to rewrite them.
func TestExportSkillsCheckedIn(t *testing.T) {
	s, err := AppSet()
	if err != nil {
		t.Fatal(err)
	}
	if *updateSkills {
		if err := ExportSkills(s, repoSkills); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckSkills(s, os.DirFS(repoSkills)); err != nil {
		t.Fatal(err)
	}
}

// rule: ARC-I-046
// Exporting skills adds no connection: the manifest still holds only the app-API connector.
func TestARC_I_046_ManifestStillGrantsNoSendConnector(t *testing.T) {
	raw, err := os.ReadFile(repoManifest)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		App struct {
			Connections []struct {
				Connector string `yaml:"connector"`
			} `yaml:"connections"`
		} `yaml:"app"`
	}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.App.Connections) != 1 || m.App.Connections[0].Connector != "counsel/app-api" {
		t.Fatalf("connections = %+v, want only counsel/app-api", m.App.Connections)
	}
}
