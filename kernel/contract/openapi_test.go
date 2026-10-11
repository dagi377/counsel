// Package contract checks that the frozen HTTP contract in api/openapi.yaml only changes
// additively against its baseline, spec/eng/a2-interfaces.openapi.yaml.
package contract

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	contractPath  = "../../api/openapi.yaml"
	baselinePath  = "../../spec/eng/a2-interfaces.openapi.yaml"
	additivePath  = "additive.txt"
	questionsPath = "../../docs/QUESTIONS.md"
)

var methods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// breakingChanges lists every change from base to head that /v1 does not allow.
// rule: ARC-I-001
func breakingChanges(base, head map[string]any) []string {
	var out []string
	basePaths, headPaths := asMap(base["paths"]), asMap(head["paths"])
	for _, p := range sortedKeys(basePaths) {
		hp, ok := headPaths[p]
		if !ok {
			out = append(out, "path removed: "+p)
			continue
		}
		bo, ho := asMap(basePaths[p]), asMap(hp)
		for _, m := range methods {
			if _, ok := bo[m]; ok {
				if _, ok := ho[m]; !ok {
					out = append(out, fmt.Sprintf("operation removed: %s %s", strings.ToUpper(m), p))
				}
			}
		}
	}
	baseComp, headComp := asMap(base["components"]), asMap(head["components"])
	for _, kind := range sortedKeys(baseComp) {
		bk, hk := asMap(baseComp[kind]), asMap(headComp[kind])
		for _, name := range sortedKeys(bk) {
			if _, ok := hk[name]; !ok {
				out = append(out, fmt.Sprintf("component removed: %s/%s", kind, name))
			}
		}
	}
	return append(out, diffNode("#", base, head)...)
}

// additions lists every key or list item in head that base lacks, as RFC 6901 JSON
// pointers. An added key is reported once; its subtree is not walked.
func additions(at string, base, head any) []string {
	var out []string
	switch h := head.(type) {
	case map[string]any:
		b, ok := base.(map[string]any)
		if !ok {
			return nil
		}
		for _, k := range sortedKeys(h) {
			p := at + "/" + pointerEscape(k)
			if bv, ok := b[k]; ok {
				out = append(out, additions(p, bv, h[k])...)
			} else {
				out = append(out, p)
			}
		}
	case []any:
		b, ok := base.([]any)
		if !ok {
			return nil
		}
		for i, v := range h {
			if i < len(b) {
				out = append(out, additions(fmt.Sprintf("%s/%d", at, i), b[i], v)...)
			} else {
				out = append(out, fmt.Sprintf("%s/%d", at, i))
			}
		}
	}
	return out
}

func pointerEscape(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

// approvedAdditions parses additive.txt: "<pointer> <Q-id>" per line, # comments.
func approvedAdditions(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	for n, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || !strings.HasPrefix(f[0], "/") || !strings.HasPrefix(f[1], "Q-") {
			return nil, fmt.Errorf("line %d: want \"<json pointer> <Q-id>\", got %q", n+1, line)
		}
		if _, dup := out[f[0]]; dup {
			return nil, fmt.Errorf("line %d: %s listed twice", n+1, f[0])
		}
		out[f[0]] = f[1]
	}
	return out, nil
}

// unapprovedChanges compares the additions from base to head with the approved list: an
// unlisted addition and a listed pointer that is no longer an addition are both findings.
func unapprovedChanges(base, head map[string]any, approved map[string]string) []string {
	var out []string
	found := map[string]bool{}
	for _, p := range additions("", base, head) {
		found[p] = true
		if _, ok := approved[p]; !ok {
			out = append(out, "addition not listed in "+additivePath+": "+p)
		}
	}
	for _, p := range sortedKeys(approved) {
		if !found[p] {
			out = append(out, "listed in "+additivePath+" but not an addition: "+p)
		}
	}
	return out
}

// diffNode compares two nodes at the same location, recursing through keys and list
// positions both sides share.
func diffNode(at string, base, head any) []string {
	var out []string
	switch b := base.(type) {
	case map[string]any:
		h, ok := head.(map[string]any)
		if !ok {
			return []string{at + ": object replaced"}
		}
		if bt, ok := b["type"]; ok && fmt.Sprint(bt) != fmt.Sprint(h["type"]) {
			out = append(out, fmt.Sprintf("%s: type changed %v -> %v", at, bt, h["type"]))
		}
		if props := asMap(b["properties"]); props != nil {
			hprops := asMap(h["properties"])
			for _, name := range sortedKeys(props) {
				if _, ok := hprops[name]; !ok {
					out = append(out, fmt.Sprintf("%s: field removed: %s", at, name))
				}
			}
		}
		if req := strings2(h["required"]); len(req) > 0 {
			was := strings2(b["required"])
			for _, r := range req {
				if !slices.Contains(was, r) {
					out = append(out, fmt.Sprintf("%s: field made required: %s", at, r))
				}
			}
		}
		if enum, ok := b["enum"]; ok {
			was, now := strings2(enum), strings2(h["enum"])
			for _, v := range was {
				if !slices.Contains(now, v) {
					out = append(out, fmt.Sprintf("%s: enum value removed: %s", at, v))
				}
			}
			// rule: ARC-I-002
			if h["x-extensible-enum"] != true {
				for _, v := range now {
					if !slices.Contains(was, v) {
						out = append(out, fmt.Sprintf("%s: value added to closed enum: %s", at, v))
					}
				}
			}
		}
		if ps, ok := b["parameters"].([]any); ok {
			out = append(out, diffParams(at, ps, asList(h["parameters"]))...)
		}
		for _, k := range sortedKeys(b) {
			if k == "parameters" {
				continue
			}
			if hv, ok := h[k]; ok {
				out = append(out, diffNode(at+"/"+k, b[k], hv)...)
			}
		}
	case []any:
		h := asList(head)
		for i := range min(len(b), len(h)) {
			out = append(out, diffNode(fmt.Sprintf("%s/%d", at, i), b[i], h[i])...)
		}
	}
	return out
}

// diffParams matches parameters by (in, name): a removed parameter or a new required one breaks clients.
func diffParams(at string, base, head []any) []string {
	key := func(p any) string {
		m := asMap(p)
		if r, ok := m["$ref"].(string); ok {
			return r
		}
		return fmt.Sprintf("%v:%v", m["in"], m["name"])
	}
	was := map[string]any{}
	for _, p := range base {
		was[key(p)] = p
	}
	now := map[string]any{}
	var out []string
	for _, p := range head {
		k := key(p)
		now[k] = p
		if bp, ok := was[k]; ok {
			out = append(out, diffNode(at+"/parameters/"+k, bp, p)...)
		} else if asMap(p)["required"] == true {
			out = append(out, fmt.Sprintf("%s: required parameter added: %s", at, k))
		}
	}
	for _, k := range sortedKeys(was) {
		if _, ok := now[k]; !ok {
			out = append(out, fmt.Sprintf("%s: parameter removed: %s", at, k))
		}
	}
	return out
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { l, _ := v.([]any); return l }

func strings2(v any) []string {
	var out []string
	for _, x := range asList(v) {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func load(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return parse(t, raw)
}

func parse(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	return doc
}

// clone deep-copies a parsed document so each case mutates its own copy.
func clone(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return parse(t, raw)
}

func schema(doc map[string]any, name string) map[string]any {
	return asMap(asMap(asMap(doc["components"])["schemas"])[name])
}

// rule: ARC-I-001
func TestARC_I_001_ContractIsHeaderPlusBaseline(t *testing.T) {
	got, err := os.ReadFile(filepath.FromSlash(contractPath))
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	want, err := os.ReadFile(filepath.FromSlash(baselinePath))
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var header []string
	body := got
	for bytes.HasPrefix(body, []byte("#")) {
		line, rest, _ := bytes.Cut(body, []byte("\n"))
		header = append(header, string(line))
		body = rest
	}
	// The body is the baseline, or the baseline plus additive changes each listed in
	// additive.txt with the docs/QUESTIONS.md id that approved it (e.g. Q-004).
	rawApproved, err := os.ReadFile(additivePath)
	if err != nil {
		t.Fatalf("read %s: %v", additivePath, err)
	}
	approved, err := approvedAdditions(rawApproved)
	if err != nil {
		t.Fatalf("%s: %v", additivePath, err)
	}
	var findings []string
	if !bytes.Equal(body, want) || len(approved) > 0 {
		var head, base map[string]any
		if err := yaml.Unmarshal(body, &head); err != nil {
			t.Fatalf("parse contract body: %v", err)
		}
		if err := yaml.Unmarshal(want, &base); err != nil {
			t.Fatalf("parse baseline: %v", err)
		}
		findings = append(breakingChanges(base, head), unapprovedChanges(base, head, approved)...)
	}
	questions, err := os.ReadFile(filepath.FromSlash(questionsPath))
	if err != nil {
		t.Fatalf("read %s: %v", questionsPath, err)
	}
	for _, p := range sortedKeys(approved) {
		if !bytes.Contains(questions, []byte("## "+approved[p]+" ")) {
			findings = append(findings, fmt.Sprintf("%s cites %s, which docs/QUESTIONS.md does not have", p, approved[p]))
		}
	}
	tests := []struct {
		name string
		ok   bool
	}{
		{name: "header present", ok: len(header) > 0},
		{name: "header says CONTRACT", ok: len(header) > 0 && strings.Contains(header[0], "CONTRACT")},
		{name: "body equals baseline plus approved additions", ok: len(findings) == 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s does not hold for %s:\n%s", tt.name, contractPath, strings.Join(findings, "\n"))
			}
		})
	}
}

// rule: ARC-I-001
func TestARC_I_001_UnlistedAdditionsFail(t *testing.T) {
	base := load(t, baselinePath)
	hook := "/paths/~1hooks~1{provider}/post/responses/403"
	tests := []struct {
		name     string
		mutate   func(doc map[string]any)
		approved map[string]string
		want     []string // findings; nil means approved
	}{
		{name: "unchanged", mutate: func(map[string]any) {}},
		{
			name: "endpoint added unlisted",
			mutate: func(d map[string]any) {
				asMap(d["paths"])["/added"] = map[string]any{"get": map[string]any{"operationId": "added"}}
			},
			want: []string{"addition not listed in additive.txt: /paths/~1added"},
		},
		{
			name: "endpoint added and listed",
			mutate: func(d map[string]any) {
				asMap(d["paths"])["/added"] = map[string]any{"get": map[string]any{"operationId": "added"}}
			},
			approved: map[string]string{"/paths/~1added": "Q-999"},
		},
		{
			name: "response field added unlisted",
			mutate: func(d map[string]any) {
				asMap(schema(d, "Bill")["properties"])["foo"] = map[string]any{"type": "integer"}
			},
			want: []string{"addition not listed in additive.txt: /components/schemas/Bill/properties/foo"},
		},
		{
			name: "extensible enum value added unlisted",
			mutate: func(d map[string]any) {
				s := schema(d, "BillStatus")
				s["enum"] = append(asList(s["enum"]), "future_state")
			},
			want: []string{fmt.Sprintf("addition not listed in additive.txt: /components/schemas/BillStatus/enum/%d", len(asList(schema(base, "BillStatus")["enum"])))},
		},
		{
			name:     "stale listing",
			mutate:   func(map[string]any) {},
			approved: map[string]string{hook: "Q-004"},
			want:     []string{"listed in additive.txt but not an addition: " + hook},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := clone(t, base)
			tt.mutate(head)
			if got := unapprovedChanges(base, head, tt.approved); !slices.Equal(got, tt.want) {
				t.Fatalf("findings = %q, want %q", got, tt.want)
			}
		})
	}
}

// rule: ARC-I-001
func TestARC_I_001_ApprovedAdditionsFormat(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{name: "comments and blanks", raw: "# x\n\n/paths/~1a Q-004\n", want: map[string]string{"/paths/~1a": "Q-004"}},
		{name: "missing question id", raw: "/paths/~1a\n", wantErr: true},
		{name: "not a pointer", raw: "paths/a Q-004\n", wantErr: true},
		{name: "not a question id", raw: "/paths/~1a T-009\n", wantErr: true},
		{name: "listed twice", raw: "/paths/~1a Q-004\n/paths/~1a Q-005\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := approvedAdditions([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: ARC-I-001
func TestARC_I_001_ContractHasNoBreakingChanges(t *testing.T) {
	if diffs := breakingChanges(load(t, baselinePath), load(t, contractPath)); len(diffs) > 0 {
		t.Fatalf("api/openapi.yaml breaks /v1:\n%s", strings.Join(diffs, "\n"))
	}
}

// rule: ARC-I-001
func TestARC_I_001_ContractDiffDetectsBreakingChanges(t *testing.T) {
	base := load(t, baselinePath)
	tests := []struct {
		name     string
		mutate   func(doc map[string]any)
		breaking string // substring of the expected finding; "" means additive
	}{
		{
			name:     "Bill.number removed",
			mutate:   func(d map[string]any) { delete(asMap(schema(d, "Bill")["properties"]), "number") },
			breaking: "field removed: number",
		},
		{
			name: "required field added to BillCreate",
			mutate: func(d map[string]any) {
				s := schema(d, "BillCreate")
				s["required"] = append(asList(s["required"]), "template_id")
			},
			breaking: "field made required: template_id",
		},
		{
			name: "new required field added to BillCreate",
			mutate: func(d map[string]any) {
				s := schema(d, "BillCreate")
				asMap(s["properties"])["note"] = map[string]any{"type": "string"}
				s["required"] = append(asList(s["required"]), "note")
			},
			breaking: "field made required: note",
		},
		{
			name:     "schema removed",
			mutate:   func(d map[string]any) { delete(asMap(asMap(d["components"])["schemas"]), "BillPatch") },
			breaking: "component removed: schemas/BillPatch",
		},
		{
			name:     "path removed",
			mutate:   func(d map[string]any) { delete(asMap(d["paths"]), firstKey(asMap(d["paths"]))) },
			breaking: "path removed",
		},
		{
			name: "field type changed",
			mutate: func(d map[string]any) {
				asMap(schema(d, "Bill")["properties"])["version"] = map[string]any{"type": "string"}
			},
			breaking: "type changed",
		},
		{
			name: "enum value removed",
			mutate: func(d map[string]any) {
				s := schema(d, "BillStatus")
				s["enum"] = asList(s["enum"])[1:]
			},
			breaking: "enum value removed: draft",
		},
		{
			name: "optional field added to BillCreate",
			mutate: func(d map[string]any) {
				asMap(schema(d, "BillCreate")["properties"])["note"] = map[string]any{"type": "string"}
			},
		},
		{
			name: "response field added to Bill",
			mutate: func(d map[string]any) {
				asMap(schema(d, "Bill")["properties"])["foo"] = map[string]any{"type": "integer"}
			},
		},
		{
			name: "endpoint added",
			mutate: func(d map[string]any) {
				asMap(d["paths"])["/added"] = map[string]any{"get": map[string]any{"operationId": "added"}}
			},
		},
		{
			name:   "unchanged",
			mutate: func(map[string]any) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := clone(t, base)
			tt.mutate(head)
			diffs := strings.Join(breakingChanges(base, head), "\n")
			if tt.breaking == "" {
				if diffs != "" {
					t.Fatalf("additive change reported as breaking:\n%s", diffs)
				}
				return
			}
			if !strings.Contains(diffs, tt.breaking) {
				t.Fatalf("want finding %q, got:\n%s", tt.breaking, diffs)
			}
		})
	}
}

func firstKey(m map[string]any) string { return sortedKeys(m)[0] }

// rule: ARC-I-002
func TestARC_I_002_EnumValuesAddOnlyWhenExtensible(t *testing.T) {
	tests := []struct {
		name       string
		extensible bool
		breaking   bool
	}{
		{name: "x-extensible-enum accepts new value", extensible: true, breaking: false},
		{name: "closed enum rejects new value", extensible: false, breaking: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := func(values ...any) map[string]any {
				s := map[string]any{"type": "string", "enum": values}
				if tt.extensible {
					s["x-extensible-enum"] = true
				}
				return map[string]any{"components": map[string]any{"schemas": map[string]any{"Status": s}}}
			}
			diffs := breakingChanges(doc("a", "b"), doc("a", "b", "future_state"))
			if got := len(diffs) > 0; got != tt.breaking {
				t.Fatalf("breaking = %v, want %v: %v", got, tt.breaking, diffs)
			}
		})
	}
}

// rule: ARC-I-002
func TestARC_I_002_ResponsesLeaveRoomForUnknownFields(t *testing.T) {
	doc := load(t, contractPath)
	if schema(doc, "BillStatus")["x-extensible-enum"] != true {
		t.Fatalf("BillStatus must be x-extensible-enum so clients can render unknown values as Other")
	}
	var closed []string
	seen := map[string]bool{}
	var walk func(at string, node any)
	walk = func(at string, node any) {
		switch n := node.(type) {
		case map[string]any:
			if ref, ok := n["$ref"].(string); ok {
				if seen[ref] {
					return
				}
				seen[ref] = true
				walk(ref, resolve(doc, ref))
				return
			}
			if n["additionalProperties"] == false {
				closed = append(closed, at)
			}
			for _, k := range sortedKeys(n) {
				walk(at+"/"+k, n[k])
			}
		case []any:
			for i, v := range n {
				walk(fmt.Sprintf("%s/%d", at, i), v)
			}
		}
	}
	paths := asMap(doc["paths"])
	for _, p := range sortedKeys(paths) {
		for _, m := range methods {
			if op := asMap(asMap(paths[p])[m]); op != nil {
				walk(p+" "+m+" responses", op["responses"])
			}
		}
	}
	if len(closed) > 0 {
		t.Fatalf("response schemas forbid unknown fields (additionalProperties: false):\n%s", strings.Join(closed, "\n"))
	}
}

// resolve follows a local JSON pointer such as #/components/schemas/Bill.
func resolve(doc map[string]any, ref string) any {
	var node any = doc
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		node = asMap(node)[part]
	}
	return node
}
