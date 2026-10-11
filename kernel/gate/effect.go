package gate

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/jackc/pgx/v5"
)

// RunEffect runs the effect of approved gate id, in tx, at most once. The relay delivers
// at least once, so the same gate may arrive several times, also concurrently: the row lock
// on the gate serialises the deliveries, and the gate.effect_execution row (one per gate
// version) makes every delivery after the first a no-op. ran reports whether this call ran
// the handler. A gate still in its cooling-off returns ran=false and no error, so the next
// delivery tries again; an expired or unapproved gate fails closed.
func (e *PG) RunEffect(ctx context.Context, tx pgx.Tx, id GateID) (ran bool, err error) {
	clk, err := clock.From(ctx)
	if err != nil {
		return false, fmt.Errorf("effect %s: %w", id, err)
	}
	g := Gate{ID: id, Preparer: Principal{Kind: PrincipalUser}}
	var (
		status         string
		hash, jcs, sha []byte
		closedAt       *time.Time
		effectiveAt    *time.Time
		done           bool
	)
	// Lock first, then read in a new statement: under READ COMMITTED a delivery that waited
	// on the lock then sees the effect_execution row the first delivery committed.
	var locked int
	err = tx.QueryRow(ctx, `SELECT 1 FROM gate.approval_request WHERE id = $1 FOR UPDATE`, string(id)).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("effect %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return false, fmt.Errorf("effect %s: lock: %w", id, err)
	}
	err = tx.QueryRow(ctx, `
SELECT r.gate_code, r.version, r.status, r.payload_hash, r.payload_type, r.payload_id::text, r.requested_by::text,
       r.expires_at, r.closed_at, r.payload_jcs, r.payload_sha256,
       (SELECT max(d.effective_at) FROM gate.gate_decision d WHERE d.approval_id = r.id AND d.decision = 'approve'),
       EXISTS (SELECT 1 FROM gate.effect_execution x WHERE x.approval_id = r.id)
  FROM gate.approval_request r WHERE r.id = $1`, string(id)).
		Scan(&g.Kind, &g.Version, &status, &hash, &g.Subject.Kind, &g.Subject.ID, &g.Preparer.UserID,
			&g.ExpiresAt, &closedAt, &jcs, &sha, &effectiveAt, &done)
	if err != nil {
		return false, fmt.Errorf("effect %s: select: %w", id, err)
	}
	if done {
		return false, nil // an earlier delivery ran it
	}
	g.Status = Status(status)
	g.PayloadHash = "sha256:" + hex.EncodeToString(hash)
	if g.Status != StatusApproved {
		return false, fmt.Errorf("effect %s: status %s: %w", id, g.Status, ErrNotPending)
	}
	// rule: ARC-I-022
	// The approvals must have closed before the TTL ran out.
	if closedAt == nil || !closedAt.Before(g.ExpiresAt) {
		return false, fmt.Errorf("effect %s: %w", id, ErrExpired)
	}
	k, _, _, err := e.kinds.Lookup(g.Kind)
	if err != nil {
		return false, fmt.Errorf("effect %s: %w", id, err)
	}
	// The kind's cooling-off runs from when the approvals closed; a self-approval's own
	// effective_at may be later still.
	due := closedAt.Add(k.CoolingOff)
	if effectiveAt != nil && effectiveAt.After(due) {
		due = *effectiveAt
	}
	if clk.Now().Before(due) {
		return false, nil // cooling-off is not over
	}
	p, ok, err := storedPayload(jcs, sha, hash)
	if err != nil || !ok {
		// A version without stored payload bytes can not be checked against its hash.
		return false, fmt.Errorf("effect %s: stored payload: %w", id, ErrPayloadChanged)
	}
	g.Payload = p
	if err := e.runDecidedEffect(ctx, tx, g, k); err != nil {
		return false, err
	}
	return true, nil
}
