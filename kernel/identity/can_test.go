package identity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

const helpdesk = "../../test/fixtures/helpdesk"

type denyAll struct{}

func (denyAll) Visible(context.Context, Principal, Subject) (bool, error) { return false, nil }

type allowAll struct{}

func (allowAll) Visible(context.Context, Principal, Subject) (bool, error) { return true, nil }

// helpdeskMatrix is the role -> capability matrix the fixture app registers.
var helpdeskMatrix = map[string][]string{
	"agent_tier1": {"ticket.read"},
	"supervisor":  {"ticket.read", "ticket.close", "ticket.refund_approve", "ticket.report"},
	"admin":       {"ticket.read", "ticket.report"},
	"agent":       {"ticket.read", "ticket.close"},
}

func loadJSON[T any](t *testing.T, name string) []T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(helpdesk, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var out []T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return out
}

func bootHelpdesk(t *testing.T) (*Roles, []string) {
	t.Helper()
	type capJSON struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	type gateJSON struct {
		Slug     string `json:"slug"`
		Approver string `json:"approver"`
	}
	r := NewRoles()
	var caps []string
	for _, c := range loadJSON[capJSON](t, "capability.json") {
		r.Capability(registry.Capability{ID: c.ID, Description: c.Description})
		caps = append(caps, c.ID)
	}
	for _, g := range loadJSON[gateJSON](t, "gatekind.json") {
		r.GateKind(registry.GateKind{Slug: g.Slug, Approver: g.Approver})
	}
	for id, cs := range helpdeskMatrix {
		r.Role(registry.Role{ID: id, Name: id, Capabilities: cs})
	}
	r.SubjectKind(registry.SubjectKind{ID: "ticket"}, allowAll{})
	r.SubjectKind(registry.SubjectKind{ID: "hidden"}, denyAll{})
	return r, caps
}

func user(id string) Principal { return Principal{Kind: registry.PrincipalUser, UserID: id} }

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func TestCanHelpdeskMatrix(t *testing.T) {
	r, caps := bootHelpdesk(t)
	if err := r.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	assign := StaticAssignments{}
	for role := range helpdeskMatrix {
		assign[UserID("u-"+role)] = []string{role}
	}
	c := NewChecker(r, assign)
	for role, granted := range helpdeskMatrix {
		for _, capID := range caps {
			t.Run(role+"/"+capID, func(t *testing.T) {
				got, err := c.Can(context.Background(), user("u-"+role), capID, Subject{})
				if err != nil {
					t.Fatalf("can: %v", err)
				}
				if want := contains(granted, capID); got != want {
					t.Fatalf("Can(%s,%s) = %v, want %v", role, capID, got, want)
				}
			})
		}
	}
}

func TestCanErrors(t *testing.T) {
	r, _ := bootHelpdesk(t)
	c := NewChecker(r, StaticAssignments{"u1": {"supervisor"}})
	tests := []struct {
		name    string
		p       Principal
		cap     string
		s       Subject
		wantErr error
	}{
		{"unknown capability", user("u1"), "ticket.nope", Subject{}, ErrUnknownCapability},
		{"unknown subject kind", user("u1"), "ticket.read", Subject{Kind: "x", ID: "1"}, ErrUnknownSubjectKind},
		{"no principal", Principal{}, "ticket.read", Subject{}, ErrNoPrincipal},
		{"user without id", user(""), "ticket.read", Subject{}, ErrNoPrincipal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Can(context.Background(), tc.p, tc.cap, tc.s); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// rule: ARC-I-042
func TestARC_I_042_HiddenSubjectIsNotVisible(t *testing.T) {
	r, _ := bootHelpdesk(t)
	c := NewChecker(r, StaticAssignments{"u1": {"supervisor"}})
	tests := []struct {
		name    string
		s       Subject
		want    bool
		wantErr error
	}{
		{"visible subject", Subject{Kind: "ticket", ID: "T1"}, true, nil},
		{"hidden subject is 404 not 403", Subject{Kind: "hidden", ID: "T2"}, false, ErrNotVisible},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Can(context.Background(), user("u1"), "ticket.read", tc.s)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, err, tc.want, tc.wantErr)
			}
			if errors.Is(err, ErrForbidden) {
				t.Fatal("hidden subject must not report forbidden")
			}
		})
	}
}

// rule: ARC-I-036
func TestARC_I_036_AgentIsIntersection(t *testing.T) {
	r, _ := bootHelpdesk(t)
	c := NewChecker(r, StaticAssignments{"tier1": {"agent_tier1"}, "sup": {"supervisor"}})
	ctx := context.Background()
	p, err := c.AgentPrincipal(ctx, "run1", "tier1", []string{"ticket.read", "ticket.close", "ticket.read"})
	if err != nil {
		t.Fatalf("agent principal: %v", err)
	}
	if len(p.Grant) != 1 || p.Grant[0] != "ticket.read" {
		t.Fatalf("grant = %v, want [ticket.read]", p.Grant)
	}
	tests := []struct {
		name string
		p    Principal
		cap  string
		want bool
	}{
		{"granted and held", p, "ticket.read", true},
		{"not held by acting user", Principal{Kind: registry.PrincipalAgent, RunID: "r", ActingFor: "tier1", Grant: []string{"ticket.close"}}, "ticket.close", false},
		{"held but not granted", Principal{Kind: registry.PrincipalAgent, RunID: "r", ActingFor: "sup", Grant: []string{"ticket.read"}}, "ticket.report", false},
		{"held and granted", Principal{Kind: registry.PrincipalAgent, RunID: "r", ActingFor: "sup", Grant: []string{"ticket.report"}}, "ticket.report", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Can(ctx, tc.p, tc.cap, Subject{Kind: "ticket", ID: "T1"})
			if err != nil || got != tc.want {
				t.Fatalf("got (%v, %v), want %v", got, err, tc.want)
			}
		})
	}
	if _, err := c.AgentPrincipal(ctx, "", "tier1", nil); !errors.Is(err, ErrNoPrincipal) {
		t.Fatalf("empty run: err = %v", err)
	}
}

// rule: ARC-I-018
func TestARC_I_018_NoDefaultApproverForAdminOrAgent(t *testing.T) {
	ctx := context.Background()
	r, _ := bootHelpdesk(t)
	c := NewChecker(r, StaticAssignments{"sup": {"supervisor"}})
	agent := Principal{Kind: registry.PrincipalAgent, RunID: "r", ActingFor: "sup", Grant: []string{"ticket.refund_approve"}}
	if ok, err := c.Can(ctx, agent, "ticket.refund_approve", Subject{}); err != nil || ok {
		t.Fatalf("agent approve = (%v, %v), want false", ok, err)
	}
	if ok, err := c.Can(ctx, user("sup"), "ticket.refund_approve", Subject{}); err != nil || !ok {
		t.Fatalf("supervisor approve = (%v, %v), want true", ok, err)
	}
	tests := []struct {
		role    string
		wantErr bool
	}{
		{"admin", true},
		{"agent", true},
		{"supervisor", false},
	}
	for _, tc := range tests {
		t.Run(tc.role, func(t *testing.T) {
			r, _ := bootHelpdesk(t)
			r.Role(registry.Role{ID: tc.role, Capabilities: []string{"ticket.read", "ticket.refund_approve"}})
			err := r.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestValidateUnknownCapability(t *testing.T) {
	r, _ := bootHelpdesk(t)
	r.Role(registry.Role{ID: "x", Capabilities: []string{"ticket.missing"}})
	if err := r.Validate(); !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("err = %v", err)
	}
}
