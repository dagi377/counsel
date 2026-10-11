package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// extensionKinds are the registered extension data the kernel validates at boot, one
// spec/kernel/<kind>.schema.json each.
var extensionKinds = []string{
	"gatekind", "capability", "rulepack", "documentkind",
	"report", "portalpage", "tool", "retentionclass",
}

const (
	schemaDir   = "../../spec/kernel"
	helpdeskDir = "../../test/fixtures/helpdesk"
)

func compileSchema(t *testing.T, kind string) *jsonschema.Schema {
	t.Helper()
	path := filepath.Join(schemaDir, kind+".schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	url := "file:///spec/kernel/" + kind + ".schema.json"
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		t.Fatalf("add %s: %v", kind, err)
	}
	s, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile %s: %v", kind, err)
	}
	return s
}

func decodeJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return v
}

// helpdeskItems loads the fixture app's registered data for one kind as JSON values.
func helpdeskItems(t *testing.T, kind string) []any {
	t.Helper()
	if kind == "rulepack" {
		raw, err := os.ReadFile(filepath.Join(helpdeskDir, "rulepack.yaml"))
		if err != nil {
			t.Fatalf("read rulepack: %v", err)
		}
		var y any
		if err := yaml.Unmarshal(raw, &y); err != nil {
			t.Fatalf("yaml rulepack: %v", err)
		}
		// Round-trip through JSON so the validator sees JSON types.
		b, err := json.Marshal(y)
		if err != nil {
			t.Fatalf("json rulepack: %v", err)
		}
		return []any{decodeJSON(t, string(b))}
	}
	raw, err := os.ReadFile(filepath.Join(helpdeskDir, kind+".json"))
	if err != nil {
		t.Fatalf("read %s fixture: %v", kind, err)
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode %s fixture: %v", kind, err)
	}
	return items
}

func TestExtensionSchemasHaveContractHeader(t *testing.T) {
	for _, kind := range extensionKinds {
		t.Run(kind, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(schemaDir, kind+".schema.json"))
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Comment string `json:"$comment"`
			}
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			if !regexp.MustCompile(`^CONTRACT:`).MatchString(doc.Comment) {
				t.Fatalf("%s: $comment must start with CONTRACT:", kind)
			}
		})
	}
}

func TestHelpdeskFixtureValidates(t *testing.T) {
	for _, kind := range extensionKinds {
		t.Run(kind, func(t *testing.T) {
			s := compileSchema(t, kind)
			items := helpdeskItems(t, kind)
			if len(items) == 0 {
				t.Fatalf("helpdesk fixture has no %s", kind)
			}
			for i, it := range items {
				if err := s.Validate(it); err != nil {
					t.Fatalf("helpdesk %s[%d]: %v", kind, i, err)
				}
			}
		})
	}
}

// termPattern compiles the kernel term list (tools/kernel_terms.txt) into one whole-word,
// case-insensitive pattern, so the list of forbidden words lives in one place.
func termPattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "tools", "kernel_terms.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var alts []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		alts = append(alts, regexp.QuoteMeta(line))
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(alts, "|") + `)\b`)
}

// TestHelpdeskFixtureHasNoLegalTerms keeps the fixture a proof that the schemas are
// domain-free: a second app with no legal vocabulary fits them.
func TestHelpdeskFixtureHasNoLegalTerms(t *testing.T) {
	legal := termPattern(t)
	entries, err := os.ReadDir(helpdeskDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(helpdeskDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if m := legal.Find(raw); m != nil {
			t.Errorf("%s: legal term %q in fixture app", e.Name(), m)
		}
	}
}

func TestExtensionSchemasReject(t *testing.T) {
	tests := []struct {
		name string
		kind string
		doc  string
	}{
		{"capability without module prefix", "capability", `{"id":"read","description":"x"}`},
		{"capability unknown field", "capability", `{"id":"a.b","description":"x","extra":1}`},
		{"gate kind unknown risk", "gatekind", `{"slug":"a.b","approver":"a.c","risk":"huge","min_approvers":1,"ttl":"1h","payload_schema":{}}`},
		{"irreversible gate with one approver", "gatekind", `{"slug":"a.b","approver":"a.c","risk":"irreversible","min_approvers":1,"ttl":"1h","payload_schema":{}}`},
		{"gate kind bad ttl", "gatekind", `{"slug":"a.b","approver":"a.c","risk":"low","min_approvers":1,"ttl":"3 days","payload_schema":{}}`},
		{"rule pack without rules", "rulepack", `{"pack":"p","version":1,"rules":[]}`},
		{"rule pack bad day count", "rulepack", `{"pack":"p","version":1,"rules":[{"id":"r","trigger":"t","offset_days":1,"day_count":"lunar"}]}`},
		{"document kind without retention class", "documentkind", `{"id":"a.b","description":"x"}`},
		{"report without params schema", "report", `{"id":"a.b","capability":"a.c"}`},
		{"portal page relative route", "portalpage", `{"id":"a.b","route":"tickets","capability":"a.c"}`},
		{"tool unknown kind", "tool", `{"name":"t","kind":"execute","input":{},"capability":"a.c"}`},
		{"purgeable class without destruction gate", "retentionclass", `{"id":"a.b","min_years":1,"never_purge":false}`},
		{"negative retention years", "retentionclass", `{"id":"a.b","min_years":-1,"never_purge":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := compileSchema(t, tt.kind)
			if err := s.Validate(decodeJSON(t, tt.doc)); err == nil {
				t.Fatalf("%s accepted %s", tt.kind, tt.doc)
			}
		})
	}
}
