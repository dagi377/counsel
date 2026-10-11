package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

const pbMatter = "0190a5c3-0000-7000-8000-000000000013"

// pbRecords is a fake SubjectRecords: one record per subject id.
type pbRecords map[string]map[string]any

func (r pbRecords) SubjectRecord(_ context.Context, _, id string) (map[string]any, error) {
	rec, ok := r[id]
	if !ok {
		return nil, agenttools.ErrNotFound
	}
	return rec, nil
}

// pbBalance is a fake MatterTrustBalances holding one signed balance.
type pbBalance int64

func (b pbBalance) MatterTrustBalance(context.Context, string) (int64, error) { return int64(b), nil }

// sample returns a value that satisfies the property schema p.
func sample(p map[string]any) any {
	if e, ok := p["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}
	if c, ok := p["const"]; ok {
		return c
	}
	typ, _ := p["type"].(string)
	if ts, ok := p["type"].([]any); ok && len(ts) > 0 {
		typ, _ = ts[0].(string)
	}
	switch typ {
	case "string":
		switch p["format"] {
		case "uuid":
			return pbMatter
		case "date":
			return "2026-09-30"
		case "date-time":
			return "2026-09-30T12:00:00-04:00"
		case "email":
			return "a@example.com"
		}
		return "x"
	case "integer", "number":
		if m, ok := p["minimum"].(json.Number); ok {
			if i, err := m.Int64(); err == nil && i > 1 {
				return i
			}
		}
		if m, ok := p["exclusiveMinimum"].(json.Number); ok {
			if i, err := m.Int64(); err == nil {
				return i + 1
			}
		}
		return int64(1)
	case "boolean":
		return true
	case "array":
		items, _ := p["items"].(map[string]any)
		n := 1
		if m, ok := p["minItems"].(json.Number); ok {
			if i, err := m.Int64(); err == nil && int(i) > n {
				n = int(i)
			}
		}
		out := make([]any, n)
		for i := range out {
			v := sample(items)
			if s, ok := v.(string); ok && n > 1 {
				v = fmt.Sprintf("%s%d", s, i) // uniqueItems
				if items["format"] == "uuid" {
					v = fmt.Sprintf("0190a5c3-0000-7000-8000-%012d", i)
				}
			}
			out[i] = v
		}
		return out
	case "object", "":
		out := map[string]any{}
		props, _ := p["properties"].(map[string]any)
		for name, sub := range props {
			if m, ok := sub.(map[string]any); ok {
				out[name] = sample(m)
			}
		}
		return out
	}
	return nil
}

func compile(t *testing.T, code string, raw json.RawMessage) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	url := "mem://test/" + code + ".json"
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validate(t *testing.T, s *jsonschema.Schema, raw json.RawMessage) error {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return s.Validate(v)
}

// TestPayloadBuilders_EveryKind covers every gate kind in spec/gates.json whose payload
// schema requires a field beyond the subject: its builder fills the payload from the
// subject's record, the payload passes the kind's schema, and a missing record or a record
// without a required field opens nothing.
func TestPayloadBuilders_EveryKind(t *testing.T) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, k := range kinds {
		var s struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(k.PayloadSchema, &s); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(s.Required, func(f string) bool { return f != "subject_type" && f != "subject_id" }) {
			want = append(want, k.Code)
		}
	}
	slices.Sort(want)
	if len(want) == 0 {
		t.Fatal("no gate kind requires a payload field")
	}
	ctx := context.Background()
	for _, k := range kinds {
		if !slices.Contains(want, k.Code) {
			continue
		}
		t.Run(k.Code, func(t *testing.T) {
			var props map[string]any
			dec := json.NewDecoder(bytes.NewReader(k.PayloadSchema))
			dec.UseNumber()
			if err := dec.Decode(&props); err != nil {
				t.Fatal(err)
			}
			full := sample(props).(map[string]any)
			var required []string
			for _, r := range props["required"].([]any) {
				required = append(required, r.(string))
			}
			schema := compile(t, k.Code, k.PayloadSchema)
			// rule: AI-R-005
			cases := []struct {
				name    string
				records pbRecords
				wantErr error
			}{
				{"fills from the record", pbRecords{"s1": full}, nil},
				{"missing record fails closed", pbRecords{}, agenttools.ErrNotFound},
				{"missing required field fails closed", pbRecords{"s1": without(full, required[0])}, agenttools.ErrPayloadSource},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					set, err := agent.PayloadBuilderSet(kinds, tc.records, pbBalance(2500))
					if err != nil {
						t.Fatal(err)
					}
					if got := set.Kinds(); !slices.Equal(got, want) {
						t.Fatalf("builder kinds = %v, want %v", got, want)
					}
					raw, err := set.Build(ctx, k.Code, registry.Subject{Kind: "document_draft", ID: "s1"})
					if tc.wantErr != nil {
						if !errors.Is(err, tc.wantErr) {
							t.Fatalf("err = %v, want %v", err, tc.wantErr)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := validate(t, schema, raw); err != nil {
						t.Fatalf("payload fails its schema: %v", err)
					}
				})
			}
		})
	}
}

func without(m map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// TestPayloadBuilders_Closing reads the G13 trust balance from the trust module. The balance
// keeps its >= 0 meaning; an overdrawn matter still opens the gate with the By-law 9 blocker.
func TestPayloadBuilders_Closing(t *testing.T) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	var schema *jsonschema.Schema
	for _, k := range kinds {
		if k.Code == agent.GateClosing {
			schema = compile(t, k.Code, k.PayloadSchema)
		}
	}
	if schema == nil {
		t.Fatal("platform.closing not in spec/gates.json")
	}
	// rule: BIL-I-010
	cases := []struct {
		name          string
		balance       int64
		wantBalance   int64
		wantOverdrawn bool
		wantShortfall int64
		wantBlockers  []string
	}{
		{"zero", 0, 0, false, 0, nil},
		{"positive", 125_000, 125_000, false, 0, nil},
		{"overdrawn", -4_200, 0, true, 4_200, []string{agent.BlockerTrustOverdrawn}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := pbRecords{"d1": {"matter_id": pbMatter}}
			set, err := agent.PayloadBuilderSet(kinds, recs, pbBalance(tc.balance))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := set.Build(context.Background(), agent.GateClosing, registry.Subject{Kind: "document_draft", ID: "d1"})
			if err != nil {
				t.Fatal(err)
			}
			if err := validate(t, schema, raw); err != nil {
				t.Fatalf("payload fails G13 schema: %v", err)
			}
			var got struct {
				MatterID  string   `json:"matter_id"`
				Balance   int64    `json:"trust_balance_cents"`
				Overdrawn bool     `json:"overdrawn"`
				Shortfall int64    `json:"shortfall_cents"`
				Blockers  []string `json:"blockers"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.MatterID != pbMatter || got.Balance != tc.wantBalance || got.Overdrawn != tc.wantOverdrawn ||
				got.Shortfall != tc.wantShortfall || !slices.Equal(got.Blockers, tc.wantBlockers) {
				t.Fatalf("payload = %+v", got)
			}
		})
	}
}

// TestPayloadBuilderSet_NeedsPorts fails closed without its ports.
func TestPayloadBuilderSet_NeedsPorts(t *testing.T) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		records  agent.SubjectRecords
		balances agent.MatterTrustBalances
	}{
		{"no records", nil, pbBalance(0)},
		{"no balances", pbRecords{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := agent.PayloadBuilderSet(kinds, tc.records, tc.balances); !errors.Is(err, agent.ErrMissingDep) {
				t.Fatalf("err = %v, want ErrMissingDep", err)
			}
		})
	}
}
