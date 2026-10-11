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

// TestARC_I_018_RegisterMatrix: the registered roles hold approver capabilities only at
// level all or approve, and admin and agent hold none.
func TestARC_I_018_RegisterMatrix(t *testing.T) {
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
		{"lawyer", "billing.approve", true},      // approve
		{"bookkeeper", "billing.payments", true}, // all
		{"lawyer", "billing.payments", false},    // view on an approver capability
		{"auditor", "billing.credit", false},     // view on an approver capability
		{"associate", "matters.open", false},     // own on an approver capability
		{"admin", "admin.users", false},          // reserved role
		{"agent", "billing.payments", false},     // reserved role
		{"associate", "matters.view", true},      // own on a plain capability
		{"auditor", "matters.directory", true},   // view on a plain capability
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
