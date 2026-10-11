package conflict

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/roles"
)

// ErrInvalidWall is returned for a wall with no matter, user or creator.
var ErrInvalidWall = errors.New("conflict: wall needs matter, user and creator")

var (
	_ Walls       = (*WallStore)(nil)
	_ roles.Walls = (*WallStore)(nil)
)

// WallStore keeps ethical walls. Rows are append-only: a wall is never edited or removed in
// place. It is the source the roles walls resolver reads through Visible, so a walled user
// sees the matter as not found and agent search omits it.
type WallStore struct {
	now func() time.Time

	mu    sync.Mutex
	walls []Wall
	index map[[2]string]bool // (userID, matterID)
}

// NewWallStore returns a WallStore that stamps CreatedAt from now.
func NewWallStore(now func() time.Time) *WallStore {
	return &WallStore{now: now, index: map[[2]string]bool{}}
}

// AddWall implements Walls. The reason is stored but never put in errors or logs.
func (s *WallStore) AddWall(_ context.Context, w Wall) error {
	w.MatterID = strings.TrimSpace(w.MatterID)
	w.UserID = strings.TrimSpace(w.UserID)
	w.CreatedBy = strings.TrimSpace(w.CreatedBy)
	if w.MatterID == "" || w.UserID == "" || w.CreatedBy == "" {
		return fmt.Errorf("add wall: %w", ErrInvalidWall)
	}
	w.CreatedAt = s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.walls = append(s.walls, w)
	s.index[[2]string{w.UserID, w.MatterID}] = true
	return nil
}

// Walled implements Walls.
func (s *WallStore) Walled(_ context.Context, userID, matterID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index[[2]string{userID, matterID}], nil
}

// Visible implements roles.Walls: a user may see a matter unless a wall screens them from it.
func (s *WallStore) Visible(ctx context.Context, userID, matterID string) (bool, error) {
	walled, err := s.Walled(ctx, userID, matterID)
	if err != nil {
		return false, err
	}
	return !walled, nil
}

// Walls lists the walls on a matter, oldest first.
func (s *WallStore) Walls(_ context.Context, matterID string) ([]Wall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Wall
	for _, w := range s.walls {
		if w.MatterID == matterID {
			out = append(out, w)
		}
	}
	return out, nil
}

// VisibleMatters returns the matter ids in ids that a is not walled from, in order. An agent
// principal is screened by the walls of the user it acts for, so agent search omits walled
// matters exactly as the user's own search would.
func VisibleMatters(ctx context.Context, w Walls, a Actor, ids []string) ([]string, error) {
	user := preparerOf(a)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		walled, err := w.Walled(ctx, user, id)
		if err != nil {
			return nil, fmt.Errorf("conflict: visible matters: %w", err)
		}
		if !walled {
			out = append(out, id)
		}
	}
	return out, nil
}
