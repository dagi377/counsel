package gate

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// revision is a checked Revise request.
type revision struct {
	hash    []byte
	changed bool
}

// prepareRevise checks a revision of a gate whose stored state is cur, without touching
// the database. Only the preparer (an agent's acting-for user counts) may revise, only a
// pending gate can be revised, and the new payload must pass the kind's schema.
func (e *PG) prepareRevise(cur Gate, curHash []byte, payload json.RawMessage, by Principal) (revision, error) {
	if cur.Status != StatusPending {
		return revision{}, fmt.Errorf("revise %s: %w", cur.ID, ErrNotPending)
	}
	if u := preparingUser(by); u == "" || u != preparingUser(cur.Preparer) {
		return revision{}, fmt.Errorf("revise %s: %w", cur.ID, ErrNotPreparer)
	}
	_, schema, _, err := e.kinds.Lookup(cur.Kind)
	if err != nil {
		return revision{}, fmt.Errorf("revise %s: %w", cur.ID, err)
	}
	doc, err := decodeJSON(payload)
	if err != nil {
		return revision{}, fmt.Errorf("revise %s: %w", cur.ID, err)
	}
	if _, ok := doc.(map[string]any); !ok {
		return revision{}, fmt.Errorf("revise %s: payload is not an object: %w", cur.ID, ErrInvalidPayload)
	}
	if err := schema.Validate(doc); err != nil {
		return revision{}, fmt.Errorf("revise %s: %w", cur.ID, ErrInvalidPayload)
	}
	sum, err := payloadSum(payload)
	if err != nil {
		return revision{}, fmt.Errorf("revise %s: %w", cur.ID, err)
	}
	return revision{hash: sum, changed: !bytes.Equal(sum, curHash)}, nil
}

// preparingUser is the user who counts as preparer for p: the user itself, or the user an
// agent run acts for. Guests never prepare.
func preparingUser(p Principal) string {
	switch p.Kind {
	case PrincipalUser:
		return p.UserID
	case PrincipalAgent:
		return p.ActingFor
	}
	return ""
}

// rule: ARC-I-015
// Revise supersedes pending gate id with a new payload version and returns the new id.
// The new row is version n+1 with supersedes_id = id; version n becomes superseded, so
// decisions on n (bound to its id and hash) never count toward n+1. An unchanged payload
// is not an edit and returns id.
func (e *PG) Revise(ctx context.Context, tx pgx.Tx, id GateID, payload json.RawMessage, by Principal) (GateID, error) {
	var (
		cur     = Gate{ID: id, Preparer: Principal{Kind: PrincipalUser}}
		curHash []byte
		status  string
	)
	err := tx.QueryRow(ctx, `
SELECT gate_code, version, status, payload_hash, requested_by::text
  FROM gate.approval_request WHERE id = $1 FOR UPDATE`, string(id)).
		Scan(&cur.Kind, &cur.Version, &status, &curHash, &cur.Preparer.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("revise %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("revise %s: select: %w", id, err)
	}
	cur.Status = Status(status)
	cur.PayloadHash = "sha256:" + hex.EncodeToString(curHash)
	r, err := e.prepareRevise(cur, curHash, payload, by)
	if err != nil {
		return "", err
	}
	if !r.changed {
		return id, nil
	}
	var next string
	if err := tx.QueryRow(ctx, `SELECT gate.supersede($1, $2)::text`, string(id), r.hash).Scan(&next); err != nil {
		return "", fmt.Errorf("revise %s: supersede: %w", id, err)
	}
	return GateID(next), nil
}
