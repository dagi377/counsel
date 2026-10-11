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
	contractPath = "../../api/openapi.yaml"
	baselinePath = "../../spec/eng/a2-interfaces.openapi.yaml"
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
	tests := []struct {
		name string
		ok   bool
	}{
		{name: "header present", ok: len(header) > 0},
		{name: "header says CONTRACT", ok: len(header) > 0 && strings.Contains(header[0], "CONTRACT")},
		{name: "body equals baseline byte for byte", ok: bytes.Equal(body, want)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s does not hold for %s", tt.name, contractPath)
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
