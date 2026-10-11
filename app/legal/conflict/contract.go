// CONTRACT: app/legal/conflict types and interfaces. Frozen once on main; changes are additive
// only and need a docs/QUESTIONS.md entry. Sources: spec/legal (conflicts area), BIL-L-043.

// Package conflict runs conflict checks, records clearance through gate G2, keeps ethical
// walls and records client ID verification.
package conflict

import (
	"context"
	"errors"
	"time"
)

// Status is the state of a check run.
type Status string

const (
	StatusOpen     Status = "open"     // run recorded, awaiting G2
	StatusCleared  Status = "cleared"  // G2 approved by a lawyer who is not the preparer
	StatusRejected Status = "rejected" // G2 rejected; the matter may not open
)

// Actor is the principal acting. An agent principal only drafts; its ActingFor user counts
// as the preparer.
type Actor struct {
	UserID    string
	IsLawyer  bool
	IsAgent   bool
	ActingFor string
}

// Request asks for a check of the given search terms (party names, aliases, companies).
type Request struct {
	MatterID string
	Terms    []string
	Preparer Actor
	// CheckID is the conflict.conflict_check record the run belongs to (Q-PORT-18, additive).
	// The Postgres run store refuses a run without it.
	CheckID string
}

// Hit is one record that matches a term.
type Hit struct {
	Term     string
	PartyID  string
	MatterID string
	Role     string // role of the party on MatterID, e.g. adverse

	// Filled by the Searcher (Q-PORT-18, additive). The Postgres run store refuses a hit
	// missing any of them; nothing defaults them.
	CheckPartyID string // the conflict.check_party searched
	MatchedKind  string // contact, party_role, sealed_prospect or lateral_disclosure; PartyID is the matched id
	MatchType    string // exact, fuzzy, phonetic, identifier or relationship
	ScorePPM     int64  // match score, 1..1000000 ppm
}

// Run is one check run. Runs are append-only: a re-check adds a new run.
type Run struct {
	ID         string
	MatterID   string
	Terms      []string
	Hits       []Hit
	PreparedBy string // user id; for an agent, the user it acts for
	Status     Status
	DecidedBy  string
	Reason     string
	RanAt      time.Time
	DecidedAt  time.Time

	// Q-PORT-18, additive. CheckID comes from Request.CheckID; Version is assigned by the
	// RunStore (1, 2, ... per check); IndexVersion is the party index version the Searcher
	// reports; Complete is true once every term was searched.
	CheckID      string
	Version      int
	IndexVersion int64
	Complete     bool
}

// Wall screens a user from a matter. Walled users get 404 on the matter and agent search omits it.
type Wall struct {
	MatterID  string
	UserID    string
	CreatedBy string
	Reason    string
	CreatedAt time.Time
}

// IDVerification records that a client's identity was verified (BIL-L-043, behind the
// conflict.id_verification flag).
type IDVerification struct {
	ClientID   string
	Method     string
	VerifiedBy string
	VerifiedAt time.Time
}

// Checker runs and lists conflict checks.
type Checker interface {
	// Check searches Terms, appends a Run in StatusOpen and returns it.
	Check(ctx context.Context, req Request) (Run, error)
	// Runs lists every run for a matter, oldest first.
	Runs(ctx context.Context, matterID string) ([]Run, error)
}

// Clearer decides gate G2 on a run.
type Clearer interface {
	// Decide clears (approve=true) or rejects an open run. The approver must be a lawyer,
	// not an agent and not the run's preparer.
	Decide(ctx context.Context, runID string, approver Actor, approve bool, reason string) (Run, error)
}

// Walls keeps ethical walls.
type Walls interface {
	AddWall(ctx context.Context, w Wall) error
	// Walled reports whether userID is screened from matterID.
	Walled(ctx context.Context, userID, matterID string) (bool, error)
}

// IDVerifier records and reads client ID verification.
type IDVerifier interface {
	RecordVerification(ctx context.Context, v IDVerification) error
	// RequireVerified returns ErrIDNotVerified unless the client has a verification.
	RequireVerified(ctx context.Context, clientID string) error
}

// Service is the whole conflict surface.
type Service interface {
	Checker
	Clearer
	Walls
	IDVerifier
}

var (
	// ErrNotFound is returned for an unknown run.
	ErrNotFound = errors.New("conflict: not found")
	// ErrNoTerms is returned for a check with no search terms.
	ErrNoTerms = errors.New("conflict: no search terms")
	// ErrPreparerApproves is returned when the preparer tries to decide their own run.
	ErrPreparerApproves = errors.New("conflict: preparer cannot approve")
	// ErrAgentApproves is returned when an agent principal tries to decide.
	ErrAgentApproves = errors.New("conflict: agent cannot approve")
	// ErrNotLawyer is returned when a non-lawyer tries to decide G2.
	ErrNotLawyer = errors.New("conflict: approver must be a lawyer")
	// ErrDecided is returned when a run is no longer open.
	ErrDecided = errors.New("conflict: run already decided")
	// ErrIDNotVerified is returned when a client has no ID verification.
	ErrIDNotVerified = errors.New("conflict: client id not verified")
)
