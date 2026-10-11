package roles_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

const specAccess = "../../../spec/legal/access.json"

// specMatrix is access.json's roles, capabilities and grants.
type specMatrix struct {
	Roles  []struct{ ID string } `json:"roles"`
	Matrix struct {
		Capabilities []struct{ ID string }        `json:"capabilities"`
		Grants       map[string]map[string]string `json:"grants"`
	} `json:"matrix"`
}

// assignments maps each user id to one role; GrantsOf serves custom roles like
// identity.PGAssignments does from core.role_grant.
type assignments struct {
	users  map[string]string
	custom map[string]map[string]string
}

func (a assignments) RolesOf(_ context.Context, u identity.UserID) ([]string, error) {
	if r, ok := a.users[string(u)]; ok {
		return []string{r}, nil
	}
	return nil, nil
}

func (a assignments) GrantsOf(_ context.Context, role string) (map[string]string, error) {
	return a.custom[role], nil
}

// checker builds the production catalog: the spec matrix and the spec gate approvers,
// loaded the way boot does, then the spec levels.
func checker(t *testing.T, a assignments) (*identity.Checker, specMatrix, map[string]bool) {
	t.Helper()
	b, err := os.ReadFile(specAccess)
	if err != nil {
		t.Fatal(err)
	}
	var m specMatrix
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	tbl, err := roles.ParseAccess(b)
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	rec := &registry.Recorder{}
	tbl.Register(rec)
	approvers := map[string]bool{}
	for _, k := range kinds {
		approvers[k.Approver] = true
		rec.GateKind(registry.GateKind{Slug: k.Code, Approver: k.Approver})
	}
	cat := identity.NewRoles()
	cat.Load(rec.Calls())
	if err := roles.ApplyLevels(cat); err != nil {
		t.Fatalf("ApplyLevels: %v", err)
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if a.users == nil {
		a.users = map[string]string{}
	}
	for _, r := range m.Roles {
		a.users[r.ID] = r.ID
	}
	return identity.NewChecker(cat, a), m, approvers
}

func user(id string) registry.Principal {
	return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
}

var levels = []roles.Level{roles.View, roles.Own, roles.All, roles.Approve}

// TestARC_I_018_SpecLevels: for every role and capability in access.json and every level a
// check can need, the checker passes exactly when the spec level covers it, except that
// admin and the agent never decide a gate (a check at all or above on an approver
// capability).
func TestARC_I_018_SpecLevels(t *testing.T) {
	c, m, approvers := checker(t, assignments{})
	ctx := context.Background()
	n := 0
	for _, r := range m.Roles {
		for _, cp := range m.Matrix.Capabilities {
			have := roles.Level(m.Matrix.Grants[r.ID][cp.ID])
			if have == "" {
				have = roles.None
			}
			for _, need := range levels {
				// rule: ARC-I-018
				want := have.Covers(need)
				if approvers[cp.ID] && need.Covers(roles.DecideLevel) && (r.ID == "admin" || r.ID == "agent") {
					want = false
				}
				got, err := c.CanAt(ctx, user(r.ID), cp.ID, string(need), registry.Subject{})
				if err != nil || got != want {
					t.Errorf("%s/%s at %s: got %v, %v; want %v (spec %s)", r.ID, cp.ID, need, got, err, want, have)
				}
				n++
			}
			got, err := c.Can(ctx, user(r.ID), cp.ID, registry.Subject{})
			if err != nil || got != (have != roles.None) {
				t.Errorf("%s/%s Can: got %v, %v; spec %s", r.ID, cp.ID, got, err, have)
			}
		}
	}
	if n == 0 {
		t.Fatal("no matrix cells checked")
	}
}

// TestARC_I_018_RoleCases pins the cases the level model exists for.
func TestARC_I_018_RoleCases(t *testing.T) {
	c, _, approvers := checker(t, assignments{})
	ctx := context.Background()
	decide := string(roles.DecideLevel)
	tests := []struct {
		name, role, capability, level string
		want                          bool
	}{
		{"associate edits matters", "associate", "matters.edit", "own", true},
		{"paralegal edits matters", "paralegal", "matters.edit", "own", true},
		{"associate opens a matter", "associate", "matters.open", "", true},
		{"associate cannot decide matter opening", "associate", "matters.open", decide, false},
		{"admin manages users", "admin", "admin.users", "", true},
		{"admin manages roles", "admin", "admin.roles", "own", true},
		{"admin never decides user activation", "admin", "admin.users", decide, false},
		{"owner decides user activation", "owner", "admin.users", decide, true},
		{"auditor views bills", "auditor", "billing.view", "view", true},
		{"auditor views credits", "auditor", "billing.credit", "view", true},
		{"auditor cannot decide credits", "auditor", "billing.credit", decide, false},
		{"lawyer decides bills", "lawyer", "billing.approve", decide, true},
		{"bookkeeper decides payments at all", "bookkeeper", "billing.payments", decide, true},
		{"lawyer views payments only", "lawyer", "billing.payments", decide, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.CanAt(ctx, user(tt.role), tt.capability, tt.level, registry.Subject{})
			if err != nil || got != tt.want {
				t.Fatalf("CanAt = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	// The auditor views every approver capability it is granted and decides none.
	for capID := range approvers {
		got, err := c.CanAt(ctx, user("auditor"), capID, decide, registry.Subject{})
		if err != nil || got {
			t.Errorf("auditor decides %s: %v, %v", capID, got, err)
		}
	}
}

// TestARC_I_018_AgentNeverDecides: an agent acting for an owner keeps the owner's working
// capabilities but never passes a decision check.
func TestARC_I_018_AgentNeverDecides(t *testing.T) {
	c, _, _ := checker(t, assignments{})
	ctx := context.Background()
	p, err := c.AgentPrincipal(ctx, "run-1", "owner", []string{"matters.edit", "billing.approve"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		capability, level string
		want              bool
	}{
		{"matters.edit", "own", true},
		{"billing.approve", "", true},
		{"billing.approve", string(roles.DecideLevel), false},
		{"billing.approve", string(roles.Approve), false},
	}
	for _, tt := range tests {
		t.Run(tt.capability+"@"+tt.level, func(t *testing.T) {
			got, err := c.CanAt(ctx, p, tt.capability, tt.level, registry.Subject{})
			if err != nil || got != tt.want {
				t.Fatalf("CanAt = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

// TestCustomRoles: a custom role from the database, in the spec's shape, works at its
// levels and never above the strongest built-in role; an unknown role holds nothing.
func TestCustomRoles(t *testing.T) {
	a := assignments{
		users: map[string]string{"sc": "senior_clerk", "gh": "ghost"},
		custom: map[string]map[string]string{"senior_clerk": {
			"matters.edit":      "own",
			"billing.approve":   "approve",
			"matters.directory": "approve", // strongest built-in is all
			"made.up":           "all",
		}},
	}
	c, _, _ := checker(t, a)
	ctx := context.Background()
	decide := string(roles.DecideLevel)
	tests := []struct {
		name, user, capability, level string
		want                          bool
	}{
		{"custom edits matters", "sc", "matters.edit", "own", true},
		{"custom lacks matter close", "sc", "matters.close", "", false},
		{"custom decides bills", "sc", "billing.approve", decide, true},
		{"custom capped at strongest built-in", "sc", "matters.directory", "approve", false},
		{"custom at the capped level", "sc", "matters.directory", "all", true},
		{"unknown role fails closed", "gh", "matters.directory", "", false},
		{"unknown role cannot decide", "gh", "billing.approve", decide, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.CanAt(ctx, user(tt.user), tt.capability, tt.level, registry.Subject{})
			if err != nil || got != tt.want {
				t.Fatalf("CanAt = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
	if _, err := c.CanAt(ctx, user("sc"), "matters.edit", "bogus", registry.Subject{}); err == nil {
		t.Fatal("unknown level: want error")
	}
}
