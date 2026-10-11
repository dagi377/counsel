package docs

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/registry/registryfake"
)

func TestRegisterKinds(t *testing.T) {
	r := registryfake.New()
	if err := RegisterKinds(r); err != nil {
		t.Fatalf("RegisterKinds: %v", err)
	}
	if err := r.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	kinds := r.IDs(registry.MethodDocumentKind)
	for _, id := range []string{KindCourtForm, KindEngagementLetter, KindPrecedent, KindResearchMemo, KindBundle, KindAnalysisTable} {
		if !slices.Contains(kinds, id) {
			t.Errorf("document kind %s not registered; got %v", id, kinds)
		}
	}
	classes := map[string]registry.RetentionClass{}
	for _, c := range r.Method(registry.MethodRetentionClass) {
		rc := c.Args[0].(registry.RetentionClass)
		classes[rc.ID] = rc
	}
	tests := []struct {
		id         string
		neverPurge bool
		minYears   int
	}{
		{RetentionFinancial, true, 10},
		{RetentionAudit, true, 10},
		{RetentionFile, false, 15},
		{RetentionConflict, true, 0},
		{RetentionIdentity, false, 6},
		{RetentionLead, false, 2},
		{RetentionOps, false, 1},
		{RetentionConfig, true, 0},
		{RetentionPersonnel, false, 7},
		{RetentionPermanent, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			c, ok := classes[tt.id]
			if !ok {
				t.Fatalf("retention class %s not registered", tt.id)
			}
			if c.NeverPurge != tt.neverPurge || c.MinYears != tt.minYears {
				t.Errorf("%s = %+v, want never_purge=%v min_years=%d", tt.id, c, tt.neverPurge, tt.minYears)
			}
			if c.NeverPurge && c.DestructionGate != "" {
				t.Errorf("%s: never-purge class has destruction gate %q", tt.id, c.DestructionGate)
			}
			if !c.NeverPurge && c.DestructionGate != DestructionGate {
				t.Errorf("%s: destruction gate = %q, want %q", tt.id, c.DestructionGate, DestructionGate)
			}
		})
	}
}

func TestKindsUseRegisteredClasses(t *testing.T) {
	classes := map[string]bool{}
	for _, c := range RetentionClasses() {
		classes[c.ID] = true
	}
	tests := Kinds()
	if len(tests) != 6 {
		t.Fatalf("Kinds() = %d kinds, want 6", len(tests))
	}
	for _, k := range tests {
		t.Run(k.ID, func(t *testing.T) {
			if !classes[k.RetentionClass] {
				t.Errorf("kind %s uses unregistered class %q", k.ID, k.RetentionClass)
			}
		})
	}
}
