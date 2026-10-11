package gates

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSpecDataMatchesSpec(t *testing.T) {
	spec := readSpecGates(t)
	tests := []struct {
		name string
		got  []byte
	}{
		{"first call", SpecData()},
		{"second call", SpecData()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !bytes.Equal(tt.got, spec) {
				t.Fatalf("app/legal/gates/kinds.json differs from spec/gates.json: copy spec/gates.json to app/legal/gates/kinds.json")
			}
		})
	}
}

func TestSpecDataIsACopy(t *testing.T) {
	a := SpecData()
	a[0] = 'x'
	if b := SpecData(); b[0] == 'x' {
		t.Fatal("SpecData returned the embedded slice, not a copy")
	}
}

func TestSpecDataParsesEverySlug(t *testing.T) {
	var raw []struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(readSpecGates(t), &raw); err != nil {
		t.Fatal(err)
	}
	kinds, err := ParseKinds(SpecData())
	if err != nil {
		t.Fatalf("ParseKinds(SpecData()): %v", err)
	}
	got := map[string]bool{}
	for _, k := range kinds {
		got[k.Code] = true
	}
	if len(kinds) != len(raw) || len(raw) == 0 {
		t.Fatalf("parsed %d kinds, spec/gates.json has %d", len(kinds), len(raw))
	}
	for _, r := range raw {
		t.Run(r.Slug, func(t *testing.T) {
			if !got[r.Slug] {
				t.Fatalf("%s missing from ParseKinds(SpecData())", r.Slug)
			}
		})
	}
}
