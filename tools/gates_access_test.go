package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	gatesFile      = "../spec/gates.json"
	accessFile     = "../spec/access.json"
	interfacesFile = "../spec/eng/a2-interfaces.json"
	kernelSchemas  = "../spec/kernel"
)

type gateKind struct {
	Slug           string          `json:"slug"`
	SpecID         string          `json:"spec_id"`
	Label          string          `json:"label"`
	Approver       string          `json:"approver"`
	Risk           string          `json:"risk"`
	MinApprovers   int             `json:"min_approvers"`
	TTL            string          `json:"ttl"`
	DualWhen       string          `json:"dual_when"`
	SoleOwnerDelay string          `json:"sole_owner_delay"`
	PayloadSchema  json.RawMessage `json:"payload_schema"`
}

type capability struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

var riskRank = map[string]int{"low": 0, "medium": 1, "high": 2, "irreversible": 3}

func kernelSchema(t *testing.T, kind string) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(kernelSchemas, kind+".schema.json"))
	if err != nil {
		t.Fatalf("read %s schema: %v", kind, err)
	}
	return compileRaw(t, "file:///spec/kernel/"+kind+".schema.json", raw)
}

func compileRaw(t *testing.T, url string, raw []byte) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		t.Fatalf("add %s: %v", url, err)
	}
	s, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile %s: %v", url, err)
	}
	return s
}

// readItems decodes a JSON array file twice: as generic values for the validator and as T.
func readItems[T any](t *testing.T, path string) ([]any, []T) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var generic []any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var typed []T
	if err := json.Unmarshal(raw, &typed); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if len(generic) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return generic, typed
}

func TestRegisteredDataValidatesAgainstKernelSchemas(t *testing.T) {
	tests := []struct {
		name string
		file string
		kind string
	}{
		{"gates", gatesFile, "gatekind"},
		{"access", accessFile, "capability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := kernelSchema(t, tt.kind)
			items, _ := readItems[any](t, tt.file)
			for i, it := range items {
				if err := s.Validate(it); err != nil {
					t.Errorf("%s[%d]: %v", tt.name, i, err)
				}
			}
		})
	}
}

func TestGatesAndCapabilitiesAreConsistent(t *testing.T) {
	_, gates := readItems[gateKind](t, gatesFile)
	_, caps := readItems[capability](t, accessFile)

	capIDs := map[string]bool{}
	for _, c := range caps {
		if capIDs[c.ID] {
			t.Errorf("duplicate capability %s", c.ID)
		}
		capIDs[c.ID] = true
	}
	slugs := map[string]bool{}
	for _, g := range gates {
		if slugs[g.Slug] {
			t.Errorf("duplicate gate slug %s", g.Slug)
		}
		slugs[g.Slug] = true
		if !capIDs[g.Approver] {
			t.Errorf("%s: approver %s is not a capability in spec/access.json", g.Slug, g.Approver)
		}
		if g.SpecID == "" {
			t.Errorf("%s: spec_id missing", g.Slug)
		}
		compileRaw(t, "file:///payload/"+g.Slug+".json", g.PayloadSchema)
	}
}

// TestGatesNotWeakerThanSpec pins the owner decision of 2026-10-11: gates.json may be
// stricter than the a2 gate_kinds table, never looser, and covers exactly its kinds.
func TestGatesNotWeakerThanSpec(t *testing.T) {
	raw, err := os.ReadFile(interfacesFile)
	if err != nil {
		t.Fatal(err)
	}
	var a2 struct {
		GateKinds []struct {
			Kind         string `json:"kind"`
			SpecGate     string `json:"spec_gate"`
			Capability   string `json:"capability"`
			MinApprovers int    `json:"min_approvers"`
			Risk         string `json:"risk"`
			TTLHours     int    `json:"ttl_hours"`
		} `json:"gate_kinds"`
	}
	if err := json.Unmarshal(raw, &a2); err != nil {
		t.Fatal(err)
	}
	_, gates := readItems[gateKind](t, gatesFile)
	bySlug := map[string]gateKind{}
	for _, g := range gates {
		bySlug[g.Slug] = g
	}
	if len(bySlug) != len(a2.GateKinds) {
		t.Errorf("gates.json has %d kinds, a2 has %d", len(bySlug), len(a2.GateKinds))
	}
	for _, want := range a2.GateKinds {
		t.Run(want.Kind, func(t *testing.T) {
			got, ok := bySlug[want.Kind]
			if !ok {
				t.Fatalf("missing from gates.json")
			}
			// A normalised spec_id keeps a2's long text in label.
			if got.SpecID != want.SpecGate && got.Label != want.SpecGate {
				t.Errorf("spec_id %q, a2 %q", got.SpecID, want.SpecGate)
			}
			if got.Approver != want.Capability {
				t.Errorf("approver %q, a2 %q", got.Approver, want.Capability)
			}
			if riskRank[got.Risk] < riskRank[want.Risk] {
				t.Errorf("risk %s weaker than a2 %s", got.Risk, want.Risk)
			}
			if got.MinApprovers < want.MinApprovers {
				t.Errorf("min_approvers %d below a2 %d", got.MinApprovers, want.MinApprovers)
			}
			if wantTTL := fmt.Sprintf("%dh", want.TTLHours); got.TTL != wantTTL {
				t.Errorf("ttl %s, a2 %s", got.TTL, wantTTL)
			}
		})
	}
}

func TestGateKindSchemaRejectsBadSpecID(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		ok   bool
	}{
		{"with spec id", `{"slug":"a.b","spec_id":"G1","approver":"a.c","risk":"low","min_approvers":1,"ttl":"1h","payload_schema":{}}`, true},
		{"without spec id", `{"slug":"a.b","approver":"a.c","risk":"low","min_approvers":1,"ttl":"1h","payload_schema":{}}`, true},
		{"empty spec id", `{"slug":"a.b","spec_id":"","approver":"a.c","risk":"low","min_approvers":1,"ttl":"1h","payload_schema":{}}`, false},
		{"numeric spec id", `{"slug":"a.b","spec_id":1,"approver":"a.c","risk":"low","min_approvers":1,"ttl":"1h","payload_schema":{}}`, false},
	}
	s := kernelSchema(t, "gatekind")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tt.doc), &v); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(v); (err == nil) != tt.ok {
				t.Fatalf("valid=%v, want %v (err %v)", err == nil, tt.ok, err)
			}
		})
	}
}

func TestGateKindSchemaDualAndSoleOwnerFields(t *testing.T) {
	const base = `"slug":"a.b","approver":"a.c","risk":"high","ttl":"1h","payload_schema":{}`
	tests := []struct {
		name string
		doc  string
		ok   bool
	}{
		{"dual when", `{` + base + `,"min_approvers":1,"dual_when":"signatories>=2"}`, true},
		{"dual when one", `{` + base + `,"min_approvers":1,"dual_when":"signatories>=1"}`, false},
		{"dual when other subject", `{` + base + `,"min_approvers":1,"dual_when":"owners>=2"}`, false},
		{"dual when empty", `{` + base + `,"min_approvers":1,"dual_when":""}`, false},
		{"sole owner delay", `{` + base + `,"min_approvers":2,"sole_owner_delay":"24h"}`, true},
		{"sole owner delay bad duration", `{` + base + `,"min_approvers":2,"sole_owner_delay":"1 day"}`, false},
		{"sole owner delay with one approver", `{` + base + `,"min_approvers":1,"sole_owner_delay":"24h"}`, false},
	}
	s := kernelSchema(t, "gatekind")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tt.doc), &v); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(v); (err == nil) != tt.ok {
				t.Fatalf("valid=%v, want %v (err %v)", err == nil, tt.ok, err)
			}
		})
	}
}

// TestGatesApplyDualAndSoleOwnerDecision pins owner decision Q-002 (2026-10-11): a2 dual-mode
// gates carry dual_when, and GA9 and GA11 need two approvers or a sole owner after 24h; every
// other two-approver kind takes a sole owner's second confirmation after 1h.
func TestGatesApplyDualAndSoleOwnerDecision(t *testing.T) {
	raw, err := os.ReadFile(interfacesFile)
	if err != nil {
		t.Fatal(err)
	}
	var a2 struct {
		GateKinds []struct {
			Kind string `json:"kind"`
			Dual *int   `json:"dual_mode_min_approvers"`
		} `json:"gate_kinds"`
	}
	if err := json.Unmarshal(raw, &a2); err != nil {
		t.Fatal(err)
	}
	dual := map[string]bool{}
	for _, k := range a2.GateKinds {
		if k.Dual != nil && *k.Dual >= 2 {
			dual[k.Kind] = true
		}
	}
	if len(dual) == 0 {
		t.Fatal("a2 marks no dual-mode gates")
	}
	soleOwner := map[string]bool{"GA9": true, "GA11": true}
	_, gates := readItems[gateKind](t, gatesFile)
	for _, g := range gates {
		t.Run(g.Slug, func(t *testing.T) {
			wantDual := ""
			if dual[g.Slug] {
				wantDual = "signatories>=2"
			}
			if g.DualWhen != wantDual {
				t.Errorf("dual_when %q, want %q", g.DualWhen, wantDual)
			}
			if soleOwner[g.SpecID] {
				if g.MinApprovers != 2 || g.SoleOwnerDelay != "24h" {
					t.Errorf("min_approvers %d sole_owner_delay %q, want 2 and 24h", g.MinApprovers, g.SoleOwnerDelay)
				}
			} else if g.MinApprovers >= 2 {
				// rule: ARC-I-021
				// LAWYER AUDIT (2026-10-11): every other two-approver kind lets a sole
				// licensee confirm a second time after at least 1h.
				if g.SoleOwnerDelay != "1h" {
					t.Errorf("sole_owner_delay %q, want 1h on a two-approver kind", g.SoleOwnerDelay)
				}
			} else if g.SoleOwnerDelay != "" {
				t.Errorf("unexpected sole_owner_delay %q", g.SoleOwnerDelay)
			}
		})
	}
}
