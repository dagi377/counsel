package eventroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// Gates is the part of the gate engine a create route uses.
type Gates interface {
	Open(ctx context.Context, tx pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error)
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// Refusal is an error a prepare step answers as a problem instead of a 500.
type Refusal struct {
	Status int
	Code   string
	Title  string
}

func (e *Refusal) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Title) }

// Prepared is what a create route's prepare step wrote: the gate subject and payload.
type Prepared struct {
	Subject gate.Subject
	Payload json.RawMessage
}

// OpenGate runs prepare and opens gate kind on what it prepared, in one tx, with p as the
// preparer, and answers 202 with the openapi Gate. The route writes only the prepared row;
// the gate's effect, after a human other than p approves, does the rest.
func (rt Route) OpenGate(w http.ResponseWriter, r *http.Request, db TxRunner, gates Gates, p registry.Principal,
	kind string, minApprovers int, prepare func(ctx context.Context, tx pgx.Tx) (Prepared, error)) {
	var gid gate.GateID
	err := db.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		got, err := prepare(ctx, tx)
		if err != nil {
			return err
		}
		// rule: ARC-I-027
		// The acting user is the gate's preparer, so they can never approve it.
		gid, err = gates.Open(ctx, tx, kind, got.Subject, got.Payload, p)
		if err != nil {
			return fmt.Errorf("open %s: %w", kind, err)
		}
		return nil
	})
	var ref *Refusal
	switch {
	case errors.As(err, &ref):
		rt.Fail(w, r, ref.Status, ref.Code, ref.Title)
		return
	case err != nil:
		rt.Internal(w, r)
		return
	}
	g, err := gates.Get(r.Context(), gid)
	if err != nil {
		rt.Internal(w, r)
		return
	}
	JSON(w, http.StatusAccepted, GateView(g, minApprovers))
}

// gateStates maps engine states to the openapi GateState.
var gateStates = map[gate.Status]string{
	gate.StatusPending:          "open",
	gate.StatusApprovedCooling:  "approved_cooling",
	gate.StatusApproved:         "approved",
	gate.StatusRejected:         "rejected",
	gate.StatusChangesRequested: "returned",
	gate.StatusWithdrawn:        "withdrawn",
	gate.StatusExpired:          "expired",
	gate.StatusSuperseded:       "withdrawn",
}

// Actor is the openapi Actor.
type Actor struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ActingFor string `json:"acting_for,omitempty"`
}

// Gate is the openapi Gate a create route answers.
type Gate struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Version      int             `json:"version"`
	State        string          `json:"state"`
	Subject      map[string]any  `json:"subject"`
	Payload      json.RawMessage `json:"payload"`
	PayloadHash  string          `json:"payload_hash"`
	Preparers    []Actor         `json:"preparers"`
	MinApprovers int             `json:"min_approvers"`
	OpenedAt     string          `json:"opened_at"`
	ExpiresAt    string          `json:"expires_at"`
	Decisions    []any           `json:"decisions"`
}

// GateView renders g; minApprovers below 1 reads as 1.
func GateView(g gate.Gate, minApprovers int) Gate {
	prep := Actor{Type: string(g.Preparer.Kind), ID: g.Preparer.UserID}
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = Actor{Type: "agent", ID: g.Preparer.RunID, ActingFor: g.Preparer.ActingFor}
	}
	return Gate{
		ID: string(g.ID), Kind: g.Kind, Version: g.Version, State: gateStates[g.Status],
		Subject: map[string]any{"type": g.Subject.Kind, "id": g.Subject.ID}, Payload: g.Payload,
		PayloadHash: g.PayloadHash, Preparers: []Actor{prep}, MinApprovers: max(minApprovers, 1),
		OpenedAt:  g.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339),
		Decisions: []any{},
	}
}
