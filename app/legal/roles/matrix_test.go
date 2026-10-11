package roles

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

func TestAccessDataMatchesSpec(t *testing.T) {
	want, err := os.ReadFile(accessPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(accessData, want) {
		t.Fatal("app/legal/roles/access.json differs from spec/legal/access.json; copy it again")
	}
}

// TestRegisterMatrix: every role is registered with each capability it holds at any level
// above none; levels themselves are checked in TestARC_I_018_SpecLevels.
func TestRegisterMatrix(t *testing.T) {
	rec := &registry.Recorder{}
	if err := registerMatrix(rec); err != nil {
		t.Fatalf("registerMatrix: %v", err)
	}
	held := map[string][]string{}
	for _, c := range rec.Method(registry.MethodRole) {
		x := c.Args[0].(registry.Role)
		held[x.ID] = x.Capabilities
	}
	tests := []struct {
		role, capability string
		want             bool
	}{
		{"lawyer", "billing.payments", true},  // view
		{"associate", "matters.edit", true},   // own
		{"admin", "admin.users", true},        // all
		{"auditor", "billing.credit", true},   // view
		{"admin", "matters.view", false},      // none
		{"bookkeeper", "matters.edit", false}, // none
	}
	for _, tt := range tests {
		t.Run(tt.role+"/"+tt.capability, func(t *testing.T) {
			if got := slices.Contains(held[tt.role], tt.capability); got != tt.want {
				t.Fatalf("holds = %v, want %v", got, tt.want)
			}
		})
	}
	if n := len(rec.Method(registry.MethodCapability)); n == 0 {
		t.Fatal("no capability registered")
	}
}
