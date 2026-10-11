// Package signatoryfake is an in-memory signatory.Service and signatory.Directory for tests.
// No network, Postgres or Docker.
package signatoryfake

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/signatory"
)

var (
	_ signatory.Service   = (*Fake)(nil)
	_ signatory.Directory = (*Fake)(nil)
)

type key struct{ account, user string }

// Fake is a concurrency-safe in-memory signatory store with its own user directory.
type Fake struct {
	// Now returns the current time; nil means time.Now.
	Now func() time.Time

	mu      sync.Mutex
	seq     int
	users   map[string]signatory.User
	sigs    map[key]*signatory.Signatory
	changes map[string]*signatory.Change
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{users: map[string]signatory.User{}, sigs: map[key]*signatory.Signatory{}, changes: map[string]*signatory.Change{}}
}

// PutUser adds or replaces a directory user.
func (f *Fake) PutUser(u signatory.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[u.ID] = u
}

func (f *Fake) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// User implements signatory.Directory.
func (f *Fake) User(ctx context.Context, id string) (signatory.User, error) {
	if err := ctx.Err(); err != nil {
		return signatory.User{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return signatory.User{}, fmt.Errorf("user %s: %w", id, signatory.ErrNotFound)
	}
	return u, nil
}

// List implements signatory.Service. Removed signatories are left out.
func (f *Fake) List(ctx context.Context, accountID string) ([]signatory.Signatory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []signatory.Signatory
	for k, s := range f.sigs {
		if k.account == accountID && s.Status != signatory.StatusRemoved {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out, nil
}

// CanSign implements signatory.Service.
func (f *Fake) CanSign(ctx context.Context, accountID, userID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sigs[key{accountID, userID}]
	return ok && s.Status == signatory.StatusActive, nil
}

func (f *Fake) eligible(s signatory.Signatory) error {
	u, ok := f.users[s.UserID]
	if !ok {
		return fmt.Errorf("user %s: %w", s.UserID, signatory.ErrNotFound)
	}
	switch {
	case !u.Active:
		return signatory.ErrNotEligible
	case s.Kind == signatory.Licensee && u.Licensee:
		return nil
	case s.Kind == signatory.BondedNonLicensee && s.BondEvidence != "":
		return nil
	}
	return signatory.ErrNotEligible
}

// Propose implements signatory.Service.
func (f *Fake) Propose(ctx context.Context, c signatory.Change) (signatory.Change, error) {
	if err := ctx.Err(); err != nil {
		return signatory.Change{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range c.Add {
		if err := f.eligible(s); err != nil {
			return signatory.Change{}, fmt.Errorf("propose: %w", err)
		}
	}
	if err := f.leavesOne(c); err != nil {
		return signatory.Change{}, fmt.Errorf("propose: %w", err)
	}
	f.seq++
	c.ID, c.Status, c.ApprovedBy, c.ApprovedAt = "chg-"+strconv.Itoa(f.seq), signatory.ChangePending, "", nil
	c.Add = append([]signatory.Signatory(nil), c.Add...)
	c.Remove = append([]string(nil), c.Remove...)
	f.changes[c.ID] = &c
	return c, nil
}

// leavesOne checks the account keeps at least one active signatory after c.
func (f *Fake) leavesOne(c signatory.Change) error {
	if len(c.Remove) == 0 {
		return nil
	}
	removed := map[string]bool{}
	for _, u := range c.Remove {
		removed[u] = true
	}
	n := len(c.Add)
	for k, s := range f.sigs {
		if k.account == c.AccountID && s.Status == signatory.StatusActive && !removed[k.user] {
			n++
		}
	}
	if n == 0 {
		return signatory.ErrLastSignatory
	}
	return nil
}

// Approve implements signatory.Service.
func (f *Fake) Approve(ctx context.Context, id string, by signatory.Approver) (signatory.Change, error) {
	if err := ctx.Err(); err != nil {
		return signatory.Change{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.changes[id]
	if !ok {
		return signatory.Change{}, fmt.Errorf("approve %s: %w", id, signatory.ErrNotFound)
	}
	switch {
	case by.IsAgent:
		return signatory.Change{}, fmt.Errorf("approve %s: %w", id, signatory.ErrAgentApproval)
	case by.UserID == "" || by.UserID == c.PreparedBy:
		return signatory.Change{}, fmt.Errorf("approve %s: %w", id, signatory.ErrSelfApproval)
	case c.Status != signatory.ChangePending:
		return signatory.Change{}, fmt.Errorf("approve %s: %w", id, signatory.ErrInvalidState)
	}
	if u, ok := f.users[by.UserID]; !ok || !u.Licensee || !u.Active {
		return signatory.Change{}, fmt.Errorf("approve %s: approver: %w", id, signatory.ErrNotEligible)
	}
	// Re-check under the lock: users or signatories may have changed since Propose.
	for _, s := range c.Add {
		if err := f.eligible(s); err != nil {
			return signatory.Change{}, fmt.Errorf("approve %s: %w", id, err)
		}
	}
	if err := f.leavesOne(*c); err != nil {
		return signatory.Change{}, fmt.Errorf("approve %s: %w", id, err)
	}
	now := f.now()
	for _, s := range c.Add {
		s.AccountID, s.Status, s.Since = c.AccountID, signatory.StatusActive, now
		f.sigs[key{c.AccountID, s.UserID}] = &s
	}
	for _, u := range c.Remove {
		if s, ok := f.sigs[key{c.AccountID, u}]; ok {
			s.Status, s.Since = signatory.StatusRemoved, now
		}
	}
	c.Status, c.ApprovedBy, c.ApprovedAt = signatory.ChangeApproved, by.UserID, &now
	return *c, nil
}

// Freeze implements signatory.Service.
func (f *Fake) Freeze(ctx context.Context, userID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	now := f.now()
	for k, s := range f.sigs {
		if k.user == userID && s.Status == signatory.StatusActive {
			s.Status, s.Since = signatory.StatusFrozen, now
			n++
		}
	}
	return n, nil
}
