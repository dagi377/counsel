package gate

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Quorum is how far one gate version is from its required approvals.
type Quorum struct {
	Required  int      // the kind's MinApprovers, stored on the version
	Approvers []string // distinct human users who approved this exact payload hash
	Met       bool
}

// vote is one stored decision, as quorum counts it.
type vote struct {
	userID   string
	decision string
	hash     []byte
}

// countQuorum applies the quorum rule to the decisions on one gate version.
func countQuorum(required int, hash []byte, votes []vote) Quorum {
	q := Quorum{Required: required}
	seen := map[string]bool{}
	// rule: ARC-I-023
	// Only approvals bound to this version's hash count, and each human counts once: a
	// recommendation, a vote on another version or a repeated approval never adds to it.
	for _, v := range votes {
		if v.decision != string(VerdictApprove) || v.userID == "" || seen[v.userID] || !bytes.Equal(v.hash, hash) {
			continue
		}
		seen[v.userID] = true
		q.Approvers = append(q.Approvers, v.userID)
	}
	q.Met = required >= 1 && len(q.Approvers) >= required
	return q
}

// Quorum reports the approvals gate id has toward its required count. A gate with
// min_approvers = 2 stays pending after one approval; the effect runs only once Met.
func (e *PG) Quorum(ctx context.Context, tx pgx.Tx, id GateID) (Quorum, error) {
	var (
		required int
		hash     []byte
	)
	err := tx.QueryRow(ctx, `SELECT required_approvals, payload_hash FROM gate.approval_request WHERE id = $1`,
		string(id)).Scan(&required, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Quorum{}, fmt.Errorf("quorum %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Quorum{}, fmt.Errorf("quorum %s: select: %w", id, err)
	}
	rows, err := tx.Query(ctx, `
SELECT decided_by::text, decision, payload_hash FROM gate.gate_decision
 WHERE approval_id = $1 ORDER BY decided_at, id`, string(id))
	if err != nil {
		return Quorum{}, fmt.Errorf("quorum %s: decisions: %w", id, err)
	}
	votes, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (vote, error) {
		var v vote
		err := r.Scan(&v.userID, &v.decision, &v.hash)
		return v, err
	})
	if err != nil {
		return Quorum{}, fmt.Errorf("quorum %s: decisions: %w", id, err)
	}
	return countQuorum(required, hash, votes), nil
}

// uniqueDecision is the gate_decision constraint that keeps one decision per human per
// gate version (UNIQUE (approval_id, decided_by) in migrations/0001_init.sql).
const uniqueDecision = "gate_decision_approval_id_decided_by_key"

// AlreadyDecided maps the database's refusal of a second decision by the same human on
// the same gate version to ErrAlreadyDecided (HTTP 409 through Problem). Other errors are
// returned unchanged.
func AlreadyDecided(id GateID, err error) error {
	// rule: ARC-I-023
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == uniqueDecision {
		return fmt.Errorf("decide %s: %w", id, ErrAlreadyDecided)
	}
	return err
}
