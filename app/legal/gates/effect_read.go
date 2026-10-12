package gates

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// ErrStale means the approved payload names a version of its subject that is no longer
// current; the approval is a conflict and changes nothing. It is the kernel's
// gate.ErrStaleVersion, so the decision route answers 412 version_conflict.
var ErrStale = gate.ErrStaleVersion

// Querier is the part of pgx.Tx an effect that reads a row uses, so tests run without a
// database.
type Querier interface {
	Execer
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Matches runs a probe statement and reports whether it matched a row. A probe never changes
// data: it is an UPDATE ... SET col = col, or a statement the caller knows is a no-op.
func Matches(ctx context.Context, tx Execer, g registry.Approved, sql string, args ...any) (bool, error) {
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return false, fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return tag.RowsAffected() > 0, nil
}

// Refuse is ErrNotApprovable for g, naming why.
func Refuse(g registry.Approved, why string) error {
	return fmt.Errorf("approve %s (gate %s): %s: %w", g.Kind, g.GateID, why, ErrNotApprovable)
}

// ApproverNot is Approver plus a check that no approver is one of the users in others (the
// subject of the approval, such as the person whose access or limit changes).
func ApproverNot(g registry.Approved, others ...string) (string, error) {
	by, err := Approver(g)
	if err != nil {
		return "", err
	}
	for _, a := range g.Approvers {
		for _, o := range others {
			if o != "" && a.UserID == o {
				return "", fmt.Errorf("approve %s (gate %s): the subject user approves: %w", g.Kind, g.GateID, ErrPreparerApproves)
			}
		}
	}
	return by, nil
}

// MinApprovers refuses g when fewer than n distinct users approved it.
func MinApprovers(g registry.Approved, n int) error {
	seen := map[string]bool{}
	for _, a := range g.Approvers {
		seen[a.UserID] = true
	}
	if len(seen) < n {
		return Refuse(g, fmt.Sprintf("%d approvers, need %d", len(seen), n))
	}
	return nil
}

// Future parses an RFC 3339 instant from a payload and refuses one that is not after now.
func Future(g registry.Approved, field, v string, now time.Time) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, Refuse(g, field+" is not a date-time")
	}
	if !t.After(now) {
		return time.Time{}, Refuse(g, field+" is not in the future")
	}
	return t, nil
}

// PreparerUser is the user who prepared g: the session user, or the user an agent acted for.
func PreparerUser(g registry.Approved) string {
	if g.Preparer.Kind == registry.PrincipalAgent {
		return g.Preparer.ActingFor
	}
	return g.Preparer.UserID
}
