package identity_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// reviewGrants mixes kernel capabilities with app ones ("desk.*", from a non-legal fixture
// app) to show both reach the plans.
func reviewGrants() identity.Grants {
	return identity.Grants{
		Capabilities: []registry.Capability{
			{ID: "core.users_admin"}, {ID: "desk.refund"}, {ID: "desk.view"}, {ID: "desk.unused"},
		},
		Roles: []registry.Role{
			{ID: "admin", Capabilities: []string{"core.users_admin", "desk.view"}},
			{ID: "agent", Capabilities: []string{"desk.view", "desk.refund"}},
		},
		Assignments: map[identity.UserID][]string{
			"u1": {"admin", "agent"},
			"u2": {"agent"},
			"u3": {},
		},
	}
}

var reviewDue = clock.Date{Year: 2026, Month: time.June, Day: 30}

func TestPlanAccessReview(t *testing.T) {
	cases := []struct {
		name    string
		grants  func() identity.Grants
		want    []identity.ReviewItem
		wantErr error
	}{
		{
			name:   "every registered capability incl. app and unheld",
			grants: reviewGrants,
			want: []identity.ReviewItem{
				{Capability: "core.users_admin", Holders: []identity.Holder{{UserID: "u1", Roles: []string{"admin"}}}},
				{Capability: "desk.refund", Holders: []identity.Holder{{UserID: "u1", Roles: []string{"agent"}}, {UserID: "u2", Roles: []string{"agent"}}}},
				{Capability: "desk.unused", Holders: []identity.Holder{}},
				{Capability: "desk.view", Holders: []identity.Holder{{UserID: "u1", Roles: []string{"admin", "agent"}}, {UserID: "u2", Roles: []string{"agent"}}}},
			},
		},
		{
			name: "role with unregistered capability",
			grants: func() identity.Grants {
				g := reviewGrants()
				g.Roles = append(g.Roles, registry.Role{ID: "x", Capabilities: []string{"desk.ghost"}})
				return g
			},
			wantErr: identity.ErrUnknownCapability,
		},
		{
			name: "assignment of unregistered role",
			grants: func() identity.Grants {
				g := reviewGrants()
				g.Assignments["u4"] = []string{"ghost"}
				return g
			},
			wantErr: identity.ErrUnknownRole,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := identity.PlanAccessReview(tc.grants(), reviewDue)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.DueOn != reviewDue {
				t.Fatalf("DueOn = %v", plan.DueOn)
			}
			if !reflect.DeepEqual(plan.Items, tc.want) {
				t.Fatalf("items\n got %+v\nwant %+v", plan.Items, tc.want)
			}
		})
	}
}

func TestPlanOffboarding(t *testing.T) {
	cases := []struct {
		name      string
		user      identity.UserID
		wantRoles []string
		wantItems []identity.OffboardingItem
		wantErr   error
	}{
		{
			name:      "kernel and app grants",
			user:      "u1",
			wantRoles: []string{"admin", "agent"},
			wantItems: []identity.OffboardingItem{
				{Capability: "core.users_admin", Roles: []string{"admin"}},
				{Capability: "desk.refund", Roles: []string{"agent"}},
				{Capability: "desk.view", Roles: []string{"admin", "agent"}},
			},
		},
		{
			name:      "app only",
			user:      "u2",
			wantRoles: []string{"agent"},
			wantItems: []identity.OffboardingItem{
				{Capability: "desk.refund", Roles: []string{"agent"}},
				{Capability: "desk.view", Roles: []string{"agent"}},
			},
		},
		{name: "no roles", user: "u3", wantRoles: []string{}},
		{name: "unknown user", user: "nobody", wantErr: identity.ErrUnknownUser},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := identity.PlanOffboarding(reviewGrants(), tc.user, reviewDue)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plan.UserID != tc.user || plan.LastDay != reviewDue {
				t.Fatalf("plan header = %+v", plan)
			}
			if !reflect.DeepEqual(plan.Roles, tc.wantRoles) || !reflect.DeepEqual(plan.Items, tc.wantItems) {
				t.Fatalf("plan\n got %+v %+v\nwant %+v %+v", plan.Roles, plan.Items, tc.wantRoles, tc.wantItems)
			}
		})
	}
}
