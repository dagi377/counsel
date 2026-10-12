package boot

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/identity"
)

// rule: ARC-I-041 (LSO RPC 3.4 conflict record)
// Production builds ONE conflict Checks over Postgres and shares it: Clearance decides G2 on
// it and matter opening reads the clearance from it, so a cleared run is never invisible to
// matter opening.
func TestProductionSharesOneConflictChecks(t *testing.T) {
	k := testKernel()
	k.DB = noConnPool{dbfake.New()}
	o, err := Production{}.options(k)
	if err != nil {
		t.Fatal(err)
	}
	shared := o.HTTP.Conflicts
	if shared == nil {
		t.Fatal("production built no conflict Checks")
	}
	o.Clearance.Roles = identity.NewRoles()
	cleared, _, err := newClearance(k, &gates.Catalog{}, *o.Clearance)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		got  *conflict.Checks
	}{
		{"clearance ports", o.Clearance.Checks},
		{"clearance decides on", cleared},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != shared {
				t.Fatalf("%s uses Checks %p, want the shared %p", tt.name, tt.got, shared)
			}
		})
	}
}
