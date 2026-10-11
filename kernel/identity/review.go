package identity

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// Sentinel errors for access reviews and offboarding.
var (
	// ErrUnknownUser means an offboarding plan names a user with no role assignments on record.
	ErrUnknownUser = errors.New("identity: unknown user")
	// ErrUnknownRole means a user is assigned a role that was never registered.
	ErrUnknownRole = errors.New("identity: unknown role")
)

// Grants is the access state a review or offboarding plan reads: every registered
// capability (kernel and app alike), every role and every user's role assignments.
type Grants struct {
	Capabilities []registry.Capability
	Roles        []registry.Role
	Assignments  map[UserID][]string // user -> role ids
}

// ReviewItem is one capability and the users who hold it, with the roles that grant it.
type ReviewItem struct {
	Capability string
	Holders    []Holder // ordered by UserID
}

// Holder is one user holding a capability through one or more roles.
type Holder struct {
	UserID UserID
	Roles  []string // ordered
}

// AccessReviewPlan lists every registered capability for certification by DueOn.
type AccessReviewPlan struct {
	DueOn clock.Date
	Items []ReviewItem // one per registered capability, ordered by Capability
}

// OffboardingItem is one capability the leaving user holds and must lose at cut-off.
type OffboardingItem struct {
	Capability string
	Roles      []string // the roles that grant it, ordered
}

// OffboardingPlan lists what to revoke from a user on their last day.
type OffboardingPlan struct {
	UserID  UserID
	LastDay clock.Date
	Roles   []string          // every role to remove, ordered
	Items   []OffboardingItem // ordered by Capability
}

// holdings maps capability -> user -> roles that grant it. A role naming an unregistered
// capability fails with ErrUnknownCapability; an assignment of an unregistered role fails with ErrUnknownRole.
func (g Grants) holdings() (map[string]map[UserID][]string, error) {
	caps := make(map[string]map[UserID][]string, len(g.Capabilities))
	for _, c := range g.Capabilities {
		caps[c.ID] = map[UserID][]string{}
	}
	roles := make(map[string][]string, len(g.Roles))
	for _, r := range g.Roles {
		for _, c := range r.Capabilities {
			if _, ok := caps[c]; !ok {
				return nil, fmt.Errorf("role %q capability %q: %w", r.ID, c, ErrUnknownCapability)
			}
		}
		roles[r.ID] = r.Capabilities
	}
	for u, rs := range g.Assignments {
		for _, r := range rs {
			cs, ok := roles[r]
			if !ok {
				return nil, fmt.Errorf("user %q role %q: %w", u, r, ErrUnknownRole)
			}
			for _, c := range cs {
				if !slices.Contains(caps[c][u], r) {
					caps[c][u] = append(caps[c][u], r)
				}
			}
		}
	}
	return caps, nil
}

// PlanAccessReview lists every registered capability, including ones with no holder, and
// every user holding it, so a reviewer certifies all access, app capabilities included.
func PlanAccessReview(g Grants, due clock.Date) (AccessReviewPlan, error) {
	caps, err := g.holdings()
	if err != nil {
		return AccessReviewPlan{}, fmt.Errorf("plan access review: %w", err)
	}
	plan := AccessReviewPlan{DueOn: due, Items: make([]ReviewItem, 0, len(caps))}
	for c, users := range caps {
		item := ReviewItem{Capability: c, Holders: make([]Holder, 0, len(users))}
		for u, rs := range users {
			rs = slices.Clone(rs)
			sort.Strings(rs)
			item.Holders = append(item.Holders, Holder{UserID: u, Roles: rs})
		}
		sort.Slice(item.Holders, func(i, j int) bool { return item.Holders[i].UserID < item.Holders[j].UserID })
		plan.Items = append(plan.Items, item)
	}
	sort.Slice(plan.Items, func(i, j int) bool { return plan.Items[i].Capability < plan.Items[j].Capability })
	return plan, nil
}

// PlanOffboarding lists every role and every registered capability user holds, so cut-off
// revokes app grants as well as kernel ones.
func PlanOffboarding(g Grants, user UserID, lastDay clock.Date) (OffboardingPlan, error) {
	assigned, ok := g.Assignments[user]
	if !ok {
		return OffboardingPlan{}, fmt.Errorf("plan offboarding %q: %w", user, ErrUnknownUser)
	}
	caps, err := g.holdings()
	if err != nil {
		return OffboardingPlan{}, fmt.Errorf("plan offboarding: %w", err)
	}
	plan := OffboardingPlan{UserID: user, LastDay: lastDay, Roles: slices.Clone(assigned)}
	sort.Strings(plan.Roles)
	plan.Roles = slices.Compact(plan.Roles)
	for c, users := range caps {
		if rs, ok := users[user]; ok {
			rs = slices.Clone(rs)
			sort.Strings(rs)
			plan.Items = append(plan.Items, OffboardingItem{Capability: c, Roles: rs})
		}
	}
	sort.Slice(plan.Items, func(i, j int) bool { return plan.Items[i].Capability < plan.Items[j].Capability })
	return plan, nil
}
