package gates_test

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/registry"
)

// levelAccess holds partner's approver capability at one level. Can passes at any level
// above none, like identity.Checker.Can; CanAt passes only at the needed level or stronger.
type levelAccess struct{ held roles.Level }

func (a levelAccess) Can(ctx context.Context, p registry.Principal, c string, s registry.Subject) (bool, error) {
	ok, err := testAccessT{}.Can(ctx, p, c, s)
	return ok && a.held != roles.None, err
}

func (a levelAccess) CanAt(ctx context.Context, p registry.Principal, c, level string, s registry.Subject) (bool, error) {
	ok, err := a.Can(ctx, p, c, s)
	return ok && a.held.Covers(roles.Level(level)), err
}

// A member who holds the approver capability below all (view or own) is refused the decision
// with 403 and no effect runs; all and approve decide (ROLE-LEVELS).
func TestARC_I_018_GateDecisionNeedsDecideLevel(t *testing.T) {
	tests := []struct {
		held   roles.Level
		status int
		code   string
	}{
		// rule: ARC-I-018
		{held: roles.View, status: 403, code: gates.CodeMissingCapability},
		{held: roles.Own, status: 403, code: gates.CodeMissingCapability},
		{held: roles.All, status: 200},
		{held: roles.Approve, status: 200},
	}
	for _, tc := range tests {
		t.Run(string(tc.held), func(t *testing.T) {
			r := newDecideRigWith(t, false, levelAccess{held: tc.held})
			rec := do(r.h, "POST", "/v1/gates/"+dGateBill+"/decisions", "partner",
				decision("approve", 1, r.hashes[dGateBill], ""), hdrs(`"1"`, key1))
			if tc.status != 200 {
				checkProblem(t, rec, tc.status, tc.code)
				if *r.effects != 0 {
					t.Errorf("effects = %d, want 0", *r.effects)
				}
				return
			}
			if rec.Code != 200 {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			if *r.effects != 1 {
				t.Errorf("effects = %d, want 1", *r.effects)
			}
		})
	}
}
