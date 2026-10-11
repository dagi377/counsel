package identity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
)

type adminStore struct {
	roles       []RoleChange
	activations []UserActivation
}

func (s *adminStore) ApplyRoleChange(_ context.Context, _ pgx.Tx, c RoleChange) error {
	s.roles = append(s.roles, c)
	return nil
}

func (s *adminStore) ActivateUser(_ context.Context, _ pgx.Tx, a UserActivation) error {
	s.activations = append(s.activations, a)
	return nil
}

func user(id string) Principal { return Principal{Kind: gate.PrincipalUser, UserID: id} }

func newAdminGates(t *testing.T, now time.Time) (*gatefake.Fake, *adminStore, AdminGates) {
	t.Helper()
	caps := gatefake.Caps{
		"roles-admin": {"admin.roles"},
		"users-admin": {"admin.users"},
		"preparer":    {"admin.roles", "admin.users"},
	}
	eng := gatefake.New(func() time.Time { return now }, caps)
	store := &adminStore{}
	ag := AdminGates{Gates: eng, Store: store}
	effects := ag.Effects()
	kinds := []gate.GateKind{
		{Slug: KindRoleChange, Approver: "admin.roles", Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour, Effect: effects[KindRoleChange]},
		{Slug: KindUserActivation, Approver: "admin.users", Risk: gate.RiskHigh, MinApprovers: 1, TTL: 72 * time.Hour, Effect: effects[KindUserActivation]},
	}
	for _, k := range kinds {
		if err := eng.Register(k); err != nil {
			t.Fatal(err)
		}
	}
	return eng, store, ag
}

func TestAdminGatesRoleChangeAndActivation(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	agent := Principal{Kind: gate.PrincipalAgent, RunID: "run-1", ActingFor: "roles-admin"}
	tests := []struct {
		name     string
		kind     string
		approver Principal
		wantErr  error
		applied  bool
	}{
		{name: "GA1 role change approved by admin.roles", kind: KindRoleChange, approver: user("roles-admin"), applied: true},
		{name: "GA2 activation approved by admin.users", kind: KindUserActivation, approver: user("users-admin"), applied: true},
		{name: "GA1 approver without admin.roles", kind: KindRoleChange, approver: user("users-admin"), wantErr: gate.ErrNotAuthorized},
		{name: "GA2 approver without admin.users", kind: KindUserActivation, approver: user("roles-admin"), wantErr: gate.ErrNotAuthorized},
		{name: "GA1 preparer never approves", kind: KindRoleChange, approver: user("preparer"), wantErr: gate.ErrSelfApproval},
		{name: "GA2 agent never approves", kind: KindUserActivation, approver: agent, wantErr: gate.ErrAgentApprover},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, ag := newAdminGates(t, now)
			var (
				id  gate.GateID
				err error
			)
			if tc.kind == KindRoleChange {
				id, err = ag.ProposeRoleChange(ctx, nil, RoleChange{RoleID: "r1", Change: json.RawMessage(`{"add":["admin.users"]}`)}, user("preparer"))
			} else {
				id, err = ag.ProposeActivation(ctx, nil, UserActivation{UserID: "u9", RoleID: "r1"}, user("preparer"))
			}
			if err != nil {
				t.Fatalf("propose: %v", err)
			}
			if len(store.roles)+len(store.activations) != 0 {
				t.Fatal("change applied before approval")
			}
			g, err := eng.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if g.Kind != tc.kind {
				t.Errorf("gate kind = %q, want %q", g.Kind, tc.kind)
			}
			err = eng.Decide(ctx, nil, id, g.Version, gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: now}, tc.approver)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("decide err = %v, want %v", err, tc.wantErr)
			}
			applied := len(store.roles)+len(store.activations) == 1
			if applied != tc.applied {
				t.Errorf("applied = %v, want %v", applied, tc.applied)
			}
			if tc.applied && tc.kind == KindUserActivation && store.activations[0] != (UserActivation{UserID: "u9", RoleID: "r1"}) {
				t.Errorf("activation = %+v", store.activations[0])
			}
			if tc.applied && tc.kind == KindRoleChange && store.roles[0].RoleID != "r1" {
				t.Errorf("role change = %+v", store.roles[0])
			}
		})
	}
}

func TestAdminGatesRejectInvalidPayload(t *testing.T) {
	ctx := context.Background()
	_, store, ag := newAdminGates(t, time.Now())
	tests := []struct {
		name string
		open func() error
	}{
		{"role change without role", func() error {
			_, err := ag.ProposeRoleChange(ctx, nil, RoleChange{Change: json.RawMessage(`{}`)}, user("preparer"))
			return err
		}},
		{"role change without change", func() error {
			_, err := ag.ProposeRoleChange(ctx, nil, RoleChange{RoleID: "r1"}, user("preparer"))
			return err
		}},
		{"activation without role", func() error {
			_, err := ag.ProposeActivation(ctx, nil, UserActivation{UserID: "u1"}, user("preparer"))
			return err
		}},
		{"effect with wrong kind", func() error {
			return ag.Effects()[KindRoleChange](ctx, nil, gate.Approved{Kind: KindUserActivation, Payload: json.RawMessage(`{"user_id":"u","role_id":"r"}`)})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.open(); !errors.Is(err, ErrInvalidAdminChange) {
				t.Errorf("err = %v, want ErrInvalidAdminChange", err)
			}
		})
	}
	if len(store.roles)+len(store.activations) != 0 {
		t.Error("invalid change applied")
	}
}
