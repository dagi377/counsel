// Package matterfake is an in-memory app/legal/matter for tests: no network, no Postgres.
package matterfake

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
)

var _ matter.Service = (*Fake)(nil)

// Fake stores matters and members in memory.
type Fake struct {
	mu       sync.Mutex
	now      func() time.Time
	contacts party.Contacts
	next     int
	matters  map[matter.ID]matter.Matter
	numbers  map[string]bool
	members  map[matter.ID][]matter.Member
}

// New returns an empty fake. When contacts is not nil, Create checks that the client exists.
func New(now func() time.Time, contacts party.Contacts) *Fake {
	return &Fake{
		now:      now,
		contacts: contacts,
		matters:  map[matter.ID]matter.Matter{},
		numbers:  map[string]bool{},
		members:  map[matter.ID][]matter.Member{},
	}
}

// Create implements matter.Matters. A new matter is pending.
func (f *Fake) Create(ctx context.Context, m matter.NewMatter) (matter.Matter, error) {
	if err := ctx.Err(); err != nil {
		return matter.Matter{}, fmt.Errorf("create matter: %w", err)
	}
	if m.Number == "" || m.ShortName == "" || m.ClientID == "" || m.PracticeAreaID == "" || m.ResponsibleLawyerID == "" {
		return matter.Matter{}, fmt.Errorf("create matter: missing field: %w", matter.ErrInvalid)
	}
	vis := m.Visibility
	if vis == "" {
		vis = matter.FirmOpen
	}
	switch vis {
	case matter.FirmOpen, matter.TeamOnly, matter.Restricted:
	default:
		return matter.Matter{}, fmt.Errorf("create matter: visibility %q: %w", vis, matter.ErrInvalid)
	}
	if f.contacts != nil {
		if _, err := f.contacts.Get(ctx, m.ClientID); err != nil {
			if errors.Is(err, party.ErrNotFound) {
				return matter.Matter{}, fmt.Errorf("create matter: %w", matter.ErrUnknownClient)
			}
			return matter.Matter{}, fmt.Errorf("create matter: %w", err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.numbers[m.Number] {
		return matter.Matter{}, fmt.Errorf("create matter: %w", matter.ErrDuplicate)
	}
	f.next++
	out := matter.Matter{
		ID:                  matter.ID("matter-" + strconv.Itoa(f.next)),
		Number:              m.Number,
		ShortName:           m.ShortName,
		ClientID:            m.ClientID,
		PracticeAreaID:      m.PracticeAreaID,
		ResponsibleLawyerID: m.ResponsibleLawyerID,
		Status:              matter.Pending,
		Visibility:          vis,
		RetentionClass:      matter.DefaultRetentionClass,
		CreatedAt:           f.now(),
		RowVersion:          1,
	}
	f.matters[out.ID] = out
	f.numbers[out.Number] = true
	return out, nil
}

// AddMember implements matter.Matters.
func (f *Fake) AddMember(ctx context.Context, m matter.NewMember) (matter.Member, error) {
	if err := ctx.Err(); err != nil {
		return matter.Member{}, fmt.Errorf("add member: %w", err)
	}
	if m.UserID == "" || m.GrantedBy == "" {
		return matter.Member{}, fmt.Errorf("add member: missing user: %w", matter.ErrInvalid)
	}
	if m.UserID == m.GrantedBy {
		return matter.Member{}, fmt.Errorf("add member: %w", matter.ErrSelfGrant)
	}
	role := m.Role
	if role == "" {
		role = matter.RoleMember
	}
	switch role {
	case matter.RoleResponsible, matter.RoleOriginating, matter.RoleMember, matter.RoleStaff, matter.RoleReviewer:
	default:
		return matter.Member{}, fmt.Errorf("add member: role %q: %w", role, matter.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.matters[m.MatterID]; !ok {
		return matter.Member{}, fmt.Errorf("add member %s: %w", m.MatterID, matter.ErrNotFound)
	}
	f.next++
	out := matter.Member{
		ID:        matter.MemberID("member-" + strconv.Itoa(f.next)),
		MatterID:  m.MatterID,
		UserID:    m.UserID,
		Role:      role,
		GrantedBy: m.GrantedBy,
	}
	f.members[m.MatterID] = append(f.members[m.MatterID], out)
	return out, nil
}

// Get implements matter.Query.
func (f *Fake) Get(ctx context.Context, id matter.ID) (matter.Matter, error) {
	if err := ctx.Err(); err != nil {
		return matter.Matter{}, fmt.Errorf("get matter: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.matters[id]
	if !ok {
		return matter.Matter{}, fmt.Errorf("get matter %s: %w", id, matter.ErrNotFound)
	}
	return m, nil
}

// List implements matter.Query, ordered by number.
func (f *Fake) List(ctx context.Context, flt matter.Filter) ([]matter.Matter, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list matters: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []matter.Matter
	for _, m := range f.matters {
		if flt.ClientID != "" && m.ClientID != flt.ClientID {
			continue
		}
		if flt.Status != "" && m.Status != flt.Status {
			continue
		}
		if flt.MemberID != "" && !f.isMember(m.ID, flt.MemberID) {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (f *Fake) isMember(id matter.ID, user string) bool {
	for _, mb := range f.members[id] {
		if mb.UserID == user {
			return true
		}
	}
	return false
}

// Members implements matter.Query.
func (f *Fake) Members(ctx context.Context, id matter.ID) ([]matter.Member, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("members: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.matters[id]; !ok {
		return nil, fmt.Errorf("members %s: %w", id, matter.ErrNotFound)
	}
	return append([]matter.Member(nil), f.members[id]...), nil
}
