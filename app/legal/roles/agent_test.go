package roles

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
)

// TestAgentLimits checks an agent run holds the weaker of the agent role and its acting-for
// role, and never approves.
//
// rule: ARC-I-036
// rule: ARC-I-018
func TestAgentLimits(t *testing.T) {
	tbl, _ := loadTable(t)
	tests := []struct {
		name      string
		actingFor RoleID
		cap       string
		need      Level
		wantLevel Level
		want      bool
	}{
		{"clerk cannot billing.approve", Clerk, "billing.approve", Approve, None, false},
		{"clerk cannot billing.draft", Clerk, "billing.draft", Own, None, false},
		{"lawyer approve capped below approve", Lawyer, "billing.approve", Approve, None, false},
		{"lawyer trust.authorize never approved", Lawyer, "trust.authorize", Approve, None, false},
		{"lawyer draft narrowed to agent own", Lawyer, "billing.draft", Own, Own, true},
		{"lawyer draft all narrowed away", Lawyer, "billing.draft", All, Own, false},
		{"clerk view own matters", Clerk, "matters.view", Own, Own, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lvl, err := tbl.AgentLevel(tc.actingFor, tc.cap)
			if err != nil || lvl != tc.wantLevel {
				t.Fatalf("level = (%v, %v), want %v", lvl, err, tc.wantLevel)
			}
			got, err := tbl.AgentCan(tc.actingFor, tc.cap, tc.need)
			if err != nil || got != tc.want {
				t.Fatalf("can = (%v, %v), want %v", got, err, tc.want)
			}
		})
	}
}

// TestAgentNeverApproves sweeps every role and capability: the agent level is never
// Approve and never exceeds either role's level.
//
// rule: ARC-I-018
func TestAgentNeverApproves(t *testing.T) {
	tbl, _ := loadTable(t)
	for _, role := range tbl.roles {
		for _, c := range tbl.caps {
			lvl, err := tbl.AgentLevel(role, c.ID)
			if err != nil {
				t.Fatalf("%s/%s: %v", role, c.ID, err)
			}
			user, _ := tbl.Level(role, c.ID)
			agent, _ := tbl.Level(Agent, c.ID)
			if lvl == Approve || !user.Covers(lvl) || !agent.Covers(lvl) {
				t.Errorf("%s/%s: agent level %s exceeds user %s or agent %s", role, c.ID, lvl, user, agent)
			}
		}
	}
}

func TestAgentLimitsErrors(t *testing.T) {
	tbl, _ := loadTable(t)
	tests := []struct {
		name      string
		actingFor RoleID
		cap       string
		need      Level
		want      error
	}{
		{"unknown role", "nobody", "billing.view", View, ErrUnknownRole},
		{"unknown capability", Clerk, "nope.nothing", View, ErrUnknownCapability},
		{"invalid need", Clerk, "billing.view", "most", ErrInvalidLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tbl.AgentCan(tc.actingFor, tc.cap, tc.need); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestModulesAreA4Schemas checks the module list matches the a2 modules (now the a4 schema
// names) and every gate kind in spec/gates.json uses one, never a former a2 name.
func TestModulesAreA4Schemas(t *testing.T) {
	b, err := os.ReadFile("../../../spec/eng/a2-interfaces.json")
	if err != nil {
		t.Fatalf("read a2: %v", err)
	}
	var a2 struct {
		Modules          []struct{ ID string } `json:"modules"`
		RenamedGateKinds map[string]string     `json:"renamed_gate_kinds"`
	}
	if err := json.Unmarshal(b, &a2); err != nil {
		t.Fatalf("parse a2: %v", err)
	}
	var ids []string
	for _, m := range a2.Modules {
		ids = append(ids, m.ID)
	}
	if want, got := slices.Sorted(slices.Values(ids)), slices.Sorted(slices.Values(Modules)); !slices.Equal(want, got) {
		t.Fatalf("modules = %v, want a2 modules %v", got, want)
	}
	for old, renamed := range a2.RenamedGateKinds {
		if _, err := GateKindModule(renamed); err != nil {
			t.Errorf("renamed kind %s: %v", renamed, err)
		}
		if _, err := GateKindModule(old); !errors.Is(err, ErrUnknownModule) {
			t.Errorf("former kind %s: err = %v, want ErrUnknownModule", old, err)
		}
	}
	g, err := os.ReadFile("../../../spec/gates.json")
	if err != nil {
		t.Fatalf("read gates: %v", err)
	}
	var kinds []struct{ Slug string }
	if err := json.Unmarshal(g, &kinds); err != nil {
		t.Fatalf("parse gates: %v", err)
	}
	for _, k := range kinds {
		if _, err := GateKindModule(k.Slug); err != nil {
			t.Errorf("gates.json: %v", err)
		}
	}
	for _, bad := range []string{"", "billing", "billing.", "documents.release"} {
		if _, err := GateKindModule(bad); !errors.Is(err, ErrUnknownModule) {
			t.Errorf("%q: err = %v, want ErrUnknownModule", bad, err)
		}
	}
}
