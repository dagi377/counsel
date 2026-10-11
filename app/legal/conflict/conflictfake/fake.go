// Package conflictfake is an in-memory conflict.Service for tests of other modules.
package conflictfake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/conflict"
)

var _ conflict.Service = (*Fake)(nil)

// Party is a searchable record seeded by tests.
type Party struct {
	ID       string
	Name     string
	MatterID string
	Role     string
}

// Fake keeps runs, walls and verifications in memory. Now defaults to time.Now.
type Fake struct {
	mu       sync.Mutex
	Now      func() time.Time
	parties  []Party
	runs     []conflict.Run
	walls    []conflict.Wall
	verified map[string]conflict.IDVerification
}

// New returns a Fake that searches parties.
func New(parties ...Party) *Fake {
	return &Fake{Now: time.Now, parties: parties, verified: map[string]conflict.IDVerification{}}
}

// Check matches each term case-insensitively as a substring of party names.
func (f *Fake) Check(_ context.Context, req conflict.Request) (conflict.Run, error) {
	if len(req.Terms) == 0 {
		return conflict.Run{}, conflict.ErrNoTerms
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	run := conflict.Run{
		ID:         fmt.Sprintf("run-%d", len(f.runs)+1),
		MatterID:   req.MatterID,
		Terms:      append([]string(nil), req.Terms...),
		PreparedBy: preparer(req.Preparer),
		Status:     conflict.StatusOpen,
		RanAt:      f.Now(),
	}
	for _, term := range req.Terms {
		t := strings.ToLower(strings.TrimSpace(term))
		for _, p := range f.parties {
			if t != "" && strings.Contains(strings.ToLower(p.Name), t) {
				run.Hits = append(run.Hits, conflict.Hit{Term: term, PartyID: p.ID, MatterID: p.MatterID, Role: p.Role})
			}
		}
	}
	f.runs = append(f.runs, run)
	return run, nil
}

// Runs lists runs for matterID in insertion order.
func (f *Fake) Runs(_ context.Context, matterID string) ([]conflict.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []conflict.Run
	for _, r := range f.runs {
		if r.MatterID == matterID {
			out = append(out, r)
		}
	}
	return out, nil
}

// Decide applies G2: lawyer, not agent, not preparer, run still open.
func (f *Fake) Decide(_ context.Context, runID string, approver conflict.Actor, approve bool, reason string) (conflict.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.runs {
		r := &f.runs[i]
		if r.ID != runID {
			continue
		}
		switch {
		case approver.IsAgent:
			return *r, conflict.ErrAgentApproves
		case approver.UserID == r.PreparedBy:
			return *r, conflict.ErrPreparerApproves
		case !approver.IsLawyer:
			return *r, conflict.ErrNotLawyer
		case r.Status != conflict.StatusOpen:
			return *r, conflict.ErrDecided
		}
		r.Status = conflict.StatusRejected
		if approve {
			r.Status = conflict.StatusCleared
		}
		r.DecidedBy, r.Reason, r.DecidedAt = approver.UserID, reason, f.Now()
		return *r, nil
	}
	return conflict.Run{}, fmt.Errorf("decide %s: %w", runID, conflict.ErrNotFound)
}

// AddWall records w.
func (f *Fake) AddWall(_ context.Context, w conflict.Wall) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = f.Now()
	}
	f.walls = append(f.walls, w)
	return nil
}

// Walled reports whether a wall screens userID from matterID.
func (f *Fake) Walled(_ context.Context, userID, matterID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.walls {
		if w.UserID == userID && w.MatterID == matterID {
			return true, nil
		}
	}
	return false, nil
}

// RecordVerification stores v.
func (f *Fake) RecordVerification(_ context.Context, v conflict.IDVerification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verified[v.ClientID] = v
	return nil
}

// RequireVerified fails unless clientID has a verification.
func (f *Fake) RequireVerified(_ context.Context, clientID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.verified[clientID]; !ok {
		return conflict.ErrIDNotVerified
	}
	return nil
}

func preparer(a conflict.Actor) string {
	if a.IsAgent {
		return a.ActingFor
	}
	return a.UserID
}
