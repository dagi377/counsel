package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// ruleIDPattern is the shape every rule id in spec/rules must have.
var ruleIDPattern = regexp.MustCompile(`^(BIL-[LTCI]|ARC-[UI]|MIG)-?\d{3}$`)

// ruleSources maps each spec/rules file to the spec page whose rules it carries.
var ruleSources = map[string]string{
	"b1.json": "b1-ontario-billing-law.json",
	"b2.json": "b2-sales-tax.json",
	"b3.json": "b3-billing-engine.json",
	"a1.json": "a1-uptime.json",
	"a2.json": "a2-interfaces.json",
	"a3.json": "a3-migration.json",
}

type ruleFile struct {
	Source string            `json:"source"`
	Rules  []json.RawMessage `json:"rules"`
}

type ruleEntry struct {
	ID string `json:"id"`
}

func readRuleIDs(t *testing.T, path string) (string, []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var f ruleFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	ids := make([]string, 0, len(f.Rules))
	for i, raw := range f.Rules {
		var r ruleEntry
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("%s: rule %d: %v", path, i, err)
		}
		ids = append(ids, r.ID)
	}
	return f.Source, ids
}

func TestRuleIDPattern(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{id: "BIL-L-001", want: true},
		{id: "BIL-T-010", want: true},
		{id: "BIL-C-202", want: true},
		{id: "BIL-I-003", want: true},
		{id: "ARC-U-001", want: true},
		{id: "ARC-I-052", want: true},
		{id: "MIG-001", want: true},
		{id: "BIL-X-001", want: false},
		{id: "ARC-U-01", want: false},
		{id: "AI-R-001", want: false},
		{id: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := ruleIDPattern.MatchString(tt.id); got != tt.want {
				t.Fatalf("match(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

// TestRulesUnion checks spec/rules/*.json is exactly the union of the b1, b2, b3, a1, a2
// and a3 rules, with every id unique and well formed.
func TestRulesUnion(t *testing.T) {
	root := filepath.Join("..", "spec")
	files, err := filepath.Glob(filepath.Join(root, "rules", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		if _, ok := ruleSources[filepath.Base(f)]; !ok {
			t.Errorf("%s: not one of the expected rule files", f)
		}
	}
	seen := map[string]string{}
	for name, src := range ruleSources {
		t.Run(name, func(t *testing.T) {
			source, ids := readRuleIDs(t, filepath.Join(root, "rules", name))
			if want := "spec/eng/" + src; source != want {
				t.Errorf("source = %q, want %q", source, want)
			}
			_, want := readRuleIDs(t, filepath.Join(root, "eng", src))
			if len(ids) != len(want) {
				t.Errorf("%d rules, %s has %d", len(ids), src, len(want))
			}
			for i := range min(len(ids), len(want)) {
				if ids[i] != want[i] {
					t.Errorf("rule %d = %q, %s has %q", i, ids[i], src, want[i])
				}
			}
			for _, id := range ids {
				if !ruleIDPattern.MatchString(id) {
					t.Errorf("rule id %q does not match %s", id, ruleIDPattern)
				}
				if prev, dup := seen[id]; dup {
					t.Errorf("rule id %q in %s and %s", id, prev, name)
				}
				seen[id] = name
				got = append(got, id)
			}
		})
	}
	if len(got) == 0 {
		t.Fatal("no rule ids found")
	}
}
