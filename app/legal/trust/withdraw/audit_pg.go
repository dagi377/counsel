package withdraw

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	kaudit "github.com/dagi377/counsel/kernel/audit"
)

var _ Auditor = (*AuditLog)(nil)

// ErrNoAuditLog means an AuditLog was built without a kernel audit log; Record then fails
// closed rather than letting the action go unrecorded.
var ErrNoAuditLog = errors.New("withdraw: no audit log configured")

// AuditLog is the production Auditor: each Record appends one kernel audit event in the
// caller's transaction, so the event commits or rolls back with the withdrawal change it
// records. The event carries only the action, the withdrawal reference and the actor; amounts
// and account numbers never reach audit detail.
type AuditLog struct {
	Log kaudit.Log
}

// NewAuditLog returns an Auditor that writes through log.
func NewAuditLog(log kaudit.Log) *AuditLog { return &AuditLog{Log: log} }

// Record appends one event for action on withdrawal withdrawalID by user by, inside tx. Any
// missing field or audit error aborts the caller's work (returned wrapped).
func (a *AuditLog) Record(ctx context.Context, tx pgx.Tx, action, withdrawalID, by string) error {
	if a == nil || a.Log == nil {
		return fmt.Errorf("audit withdrawal %s: %w", action, ErrNoAuditLog)
	}
	if action == "" || withdrawalID == "" || by == "" {
		return fmt.Errorf("audit withdrawal %q: %w", action, kaudit.ErrInvalidEvent)
	}
	e := kaudit.Event{
		ActorKind: "user",
		ActorID:   by,
		Action:    action,
		Subject:   kaudit.Subject{Kind: GateKind, ID: withdrawalID},
	}
	if err := a.Log.Append(ctx, tx, e); err != nil {
		return fmt.Errorf("audit withdrawal %s: %w", action, err)
	}
	return nil
}
