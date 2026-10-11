package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// TestSchemasMatchSpec keeps the embedded tool definitions equal to spec/eng/a2-interfaces.json.
func TestSchemasMatchSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "eng", "a2-interfaces.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Tools []schemas.Def `json:"tools"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	defs, err := schemas.Defs()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != schemas.Count || len(spec.Tools) != schemas.Count {
		t.Fatalf("tools: embedded %d, spec %d, want %d", len(defs), len(spec.Tools), schemas.Count)
	}
	for i, want := range spec.Tools {
		got := defs[i]
		if got.Name != want.Name || got.Kind != want.Kind || got.Capability != want.Capability {
			t.Errorf("tool %d: got %s/%s/%s, want %s/%s/%s", i, got.Name, got.Kind, got.Capability, want.Name, want.Kind, want.Capability)
		}
		if !jsonEqual(t, got.Input, want.Input) {
			t.Errorf("%s: input schema differs from spec", want.Name)
		}
	}
}

func TestRegisterNeedsEveryHandler(t *testing.T) {
	defs, err := schemas.Defs()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		missing string
		want    error
	}{
		{"all handlers", "", nil},
		{"one missing", defs[len(defs)-1].Name, schemas.ErrMissingHandler},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := handlers(defs, nil)
			delete(h, c.missing)
			err := schemas.Register(agenttools.NewRegistry(agenttoolsfake.New()), h)
			if !errors.Is(err, c.want) {
				t.Fatalf("Register: %v, want %v", err, c.want)
			}
		})
	}
}

// TestEveryToolValidatesInput registers all 25 tools through kernel/agenttools and checks
// that a valid input reaches the handler and an invalid one is refused before it.
func TestEveryToolValidatesInput(t *testing.T) {
	defs, err := schemas.Defs()
	if err != nil {
		t.Fatal(err)
	}
	called := map[string]int{}
	reg := agenttools.NewRegistry(agenttoolsfake.New())
	if err := schemas.Register(reg, handlers(defs, called)); err != nil {
		t.Fatal(err)
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	for _, d := range defs {
		valid := minimal(t, d.Input)
		var obj map[string]any
		if err := json.Unmarshal(valid, &obj); err != nil {
			t.Fatal(err)
		}
		obj["not_in_schema"] = 1
		extra, _ := json.Marshal(obj)
		cases := []struct {
			name string
			in   string
			want error
		}{
			{"valid", string(valid), nil},
			{"unknown field", string(extra), agenttools.ErrInvalidInput},
			{"not an object", `[]`, agenttools.ErrInvalidInput},
			{"not json", `{`, agenttools.ErrInvalidInput},
		}
		for _, c := range cases {
			t.Run(d.Name+"/"+c.name, func(t *testing.T) {
				before := called[d.Name]
				_, err := reg.Call(context.Background(), user, d.Name, json.RawMessage(c.in))
				if !errors.Is(err, c.want) {
					t.Fatalf("Call(%s): %v, want %v", c.in, err, c.want)
				}
				ran := called[d.Name] > before
				if ran != (c.want == nil) {
					t.Fatalf("handler ran = %v, want %v", ran, c.want == nil)
				}
			})
		}
	}
}

func handlers(defs []schemas.Def, called map[string]int) map[string]schemas.Handler {
	h := map[string]schemas.Handler{}
	for _, d := range defs {
		name := d.Name
		// A write tool may only return a draft, a proposal or an opened gate (ARC-I-035).
		out := json.RawMessage(`{}`)
		if d.Kind == registry.ToolWrite {
			out = json.RawMessage(`{"outcome":"draft","ref":"d1"}`)
		}
		h[name] = func(_ context.Context, _ registry.Principal, _ json.RawMessage) (registry.ToolResult, error) {
			if called != nil {
				called[name]++
			}
			return registry.ToolResult{Output: out}, nil
		}
	}
	return h
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return bytes.Equal(xa, ya)
}

// minimal builds the smallest input that satisfies a tool schema: required fields only.
func minimal(t *testing.T, raw []byte) []byte {
	t.Helper()
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // integers only: no floats in app code
	if err := dec.Decode(&root); err != nil {
		t.Fatal(err)
	}
	defs, _ := root["$defs"].(map[string]any)
	v := build(t, root, defs)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var patternValues = map[string]string{
	"^sha256:[0-9a-f]{64}$": "sha256:" + strings.Repeat("0", 64),
	"^[a-z0-9-]{1,40}$":     "step-1",
}

func build(t *testing.T, s map[string]any, defs map[string]any) any {
	t.Helper()
	if ref, ok := s["$ref"].(string); ok {
		d, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if !ok {
			t.Fatalf("unresolved ref %s", ref)
		}
		return build(t, d, defs)
	}
	if c, ok := s["const"]; ok {
		return c
	}
	if e, ok := s["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}
	typ, _ := s["type"].(string)
	switch typ {
	case "object", "":
		obj := map[string]any{}
		props, _ := s["properties"].(map[string]any)
		req, _ := s["required"].([]any)
		for _, r := range req {
			p, _ := props[r.(string)].(map[string]any)
			obj[r.(string)] = build(t, p, defs)
		}
		return obj
	case "array":
		n := 0
		if m, ok := s["minItems"].(json.Number); ok {
			n = int(num(t, m))
		}
		items, _ := s["items"].(map[string]any)
		arr := make([]any, n)
		for i := range arr {
			arr[i] = build(t, items, defs)
		}
		return arr
	case "integer", "number":
		if m, ok := s["minimum"].(json.Number); ok {
			return num(t, m)
		}
		return 0
	case "boolean":
		return false
	case "string":
		if p, ok := s["pattern"].(string); ok {
			v, ok := patternValues[p]
			if !ok {
				t.Fatalf("no sample for pattern %s", p)
			}
			return v
		}
		switch s["format"] {
		case "uuid":
			return "0192f1a0-0000-7000-8000-000000000001"
		case "date":
			return "2026-10-13"
		case "date-time":
			return "2026-10-13T09:00:00-04:00"
		}
		if m, ok := s["minLength"].(json.Number); ok && num(t, m) > 0 {
			return strings.Repeat("a", int(num(t, m)))
		}
		return "a"
	}
	t.Fatalf("unsupported schema type %q", typ)
	return nil
}

func num(t *testing.T, n json.Number) int64 {
	t.Helper()
	v, err := n.Int64()
	if err != nil {
		t.Fatalf("schema number %s: %v", n, err)
	}
	return v
}

// TestHandlersUseA2Schemas keeps the read and write handlers on the a2 definitions that
// schemas.Register gives kernel/agenttools, so no handler validates against its own copy.
func TestHandlersUseA2Schemas(t *testing.T) {
	rt, err := agent.NewReadTools(agent.ReadDeps{})
	if err != nil {
		t.Fatal(err)
	}
	wt, err := agent.NewWriteTools(agent.WriteDeps{})
	if err != nil {
		t.Fatal(err)
	}
	specs := append(rt.Specs(), wt.Specs()...)
	if len(specs) == 0 {
		t.Fatal("no handler specs")
	}
	for _, s := range specs {
		t.Run(s.Name, func(t *testing.T) {
			d, err := schemas.ByName(s.Name)
			if err != nil {
				t.Fatal(err)
			}
			if s.Capability != d.Capability || string(s.Kind) != string(d.Kind) {
				t.Errorf("spec %s/%s, a2 %s/%s", s.Kind, s.Capability, d.Kind, d.Capability)
			}
			if !bytes.Equal(s.InputSchema, d.Input) {
				t.Errorf("input schema is not the a2 schema")
			}
		})
	}
}
