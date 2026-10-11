package gate

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

var _ Engine = (*PG)(nil)

// ErrNoDB is returned by Get when no database was wired with SetDB.
var ErrNoDB = errors.New("gate: no database")

// TxRunner runs fn in one database transaction. kernel/db.DB satisfies it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// SetDB installs the database Get reads from.
func (e *PG) SetDB(d TxRunner) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.db = d
}

func (e *PG) txRunner() TxRunner {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.db
}

// Get returns one gate version, read in its own transaction. A missing gate is ErrNotFound.
func (e *PG) Get(ctx context.Context, id GateID) (Gate, error) {
	d := e.txRunner()
	if d == nil {
		return Gate{}, fmt.Errorf("get %s: %w", id, ErrNoDB)
	}
	var g Gate
	err := d.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		g, err = getGate(ctx, tx, id)
		return err
	})
	if err != nil {
		return Gate{}, err
	}
	return g, nil
}

// getGate reads gate id and its approvals in tx.
func getGate(ctx context.Context, tx pgx.Tx, id GateID) (Gate, error) {
	g := Gate{ID: id}
	var (
		supersedes, runID *string
		status, requester string
		hash, jcs         []byte
		effectiveAt       *time.Time
	)
	err := tx.QueryRow(ctx, `
SELECT r.gate_code, r.version, r.supersedes_id::text, r.payload_type, r.payload_id::text, r.payload_hash, r.payload_jcs,
       r.requested_by::text, r.agent_run_id::text, r.status, r.created_at, r.expires_at,
       coalesce((SELECT bool_or(d.self_approval_exception) FROM gate.gate_decision d
                  WHERE d.approval_id = r.id AND d.decision = 'approve'), false),
       (SELECT max(d.effective_at) FROM gate.gate_decision d WHERE d.approval_id = r.id AND d.decision = 'approve'),
       EXISTS (SELECT 1 FROM gate.effect_execution x WHERE x.approval_id = r.id AND x.outcome = 'ok')
  FROM gate.approval_request r WHERE r.id = $1`, string(id)).
		Scan(&g.Kind, &g.Version, &supersedes, &g.Subject.Kind, &g.Subject.ID, &hash, &jcs,
			&requester, &runID, &status, &g.CreatedAt, &g.ExpiresAt,
			&g.SelfApproved, &effectiveAt, &g.EffectRan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Gate{}, fmt.Errorf("get %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Gate{}, fmt.Errorf("get %s: select: %w", id, err)
	}
	if supersedes != nil {
		g.Supersedes = GateID(*supersedes)
	}
	g.Status = Status(status)
	g.PayloadHash = "sha256:" + hex.EncodeToString(hash)
	if jcs != nil {
		g.Payload = json.RawMessage(jcs)
	}
	// An agent's acting-for user is stored as the requester; report the run as preparer.
	g.Preparer = Principal{Kind: PrincipalUser, UserID: requester}
	if runID != nil {
		g.Preparer = Principal{Kind: PrincipalAgent, RunID: *runID, ActingFor: requester}
	}
	if effectiveAt != nil && g.Status == StatusApproved {
		g.EffectiveAt = *effectiveAt
	}
	if g.Approvers, err = approversOf(ctx, tx, id); err != nil {
		return Gate{}, err
	}
	return g, nil
}

// Withdraw closes pending gate id on the preparer's request, in tx. Only the preparer (an
// agent's acting-for user counts) may withdraw, and only while the gate is pending.
func (e *PG) Withdraw(ctx context.Context, tx pgx.Tx, id GateID, by Principal) error {
	var status, requester string
	err := tx.QueryRow(ctx, `
SELECT status, requested_by::text FROM gate.approval_request WHERE id = $1 FOR UPDATE`, string(id)).
		Scan(&status, &requester)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("withdraw %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("withdraw %s: select: %w", id, err)
	}
	if err := checkWithdraw(id, Status(status), requester, by); err != nil {
		return err
	}
	// The approval_request guard lets status change only inside the gate engine's own scope.
	if _, err := tx.Exec(ctx, `SELECT set_config('counsel.gate_tx', 'on', true)`); err != nil {
		return fmt.Errorf("withdraw %s: scope: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE gate.approval_request SET status = 'withdrawn', closed_at = now() WHERE id = $1 AND status = 'pending'`,
		string(id)); err != nil {
		return fmt.Errorf("withdraw %s: update: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('counsel.gate_tx', 'off', true)`); err != nil {
		return fmt.Errorf("withdraw %s: scope: %w", id, err)
	}
	return nil
}

// checkWithdraw applies the withdraw rules without touching the database.
func checkWithdraw(id GateID, status Status, requester string, by Principal) error {
	if u := preparingUser(by); u == "" || u != requester {
		return fmt.Errorf("withdraw %s: %w", id, ErrNotPreparer)
	}
	if status != StatusPending {
		return fmt.Errorf("withdraw %s: status %s: %w", id, status, ErrNotPending)
	}
	return nil
}

// RunDue expires pending gates past their TTL, then runs the effect of every approved gate
// whose effect has not run and whose cooling-off has ended, all in tx. It returns how many
// effects ran. A gate whose effect fails is recorded as failed by RunEffect and skipped, so
// one bad gate does not hold back the rest; database errors abort.
func (e *PG) RunDue(ctx context.Context, tx pgx.Tx) (int, error) {
	if _, err := e.ExpireDue(ctx, tx); err != nil {
		return 0, fmt.Errorf("run due: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT r.id::text FROM gate.approval_request r
 WHERE r.status = 'approved' AND r.closed_at < r.expires_at
   AND NOT EXISTS (SELECT 1 FROM gate.effect_execution x WHERE x.approval_id = r.id)
 ORDER BY r.closed_at, r.id`)
	if err != nil {
		return 0, fmt.Errorf("run due: select: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("run due: select: %w", err)
	}
	n := 0
	for _, id := range ids {
		ran, err := e.RunEffect(ctx, tx, GateID(id))
		switch {
		case errors.Is(err, ErrEffectFailed), errors.Is(err, ErrUnknownKind), errors.Is(err, ErrPayloadChanged):
			slog.WarnContext(ctx, "gate effect not run", "gate_id", id, "err", err)
			continue
		case err != nil:
			return n, fmt.Errorf("run due: %w", err)
		}
		if ran {
			n++
		}
	}
	return n, nil
}
