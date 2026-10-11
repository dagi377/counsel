package timekeeping

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/flags"
)

// FlagParalegalScopeVerified gates the paralegal scope check until the LSO rule is confirmed.
// It mirrors the app/legal flag of the same name and is off by default.
const FlagParalegalScopeVerified = "timekeeping.paralegal_scope_verified"

// FlagOutOfScope is the entry flag set on a paralegal entry outside the scope list.
const FlagOutOfScope = "W-SCOPE"

// Scope is the configured paralegal scope: which users are paralegal timekeepers and which
// activity codes fall inside the paralegal scope of practice.
type Scope struct {
	Paralegals map[string]bool // user id -> paralegal timekeeper
	Activities map[string]bool // activity code -> inside the paralegal scope
}

var _ ScopeChecker = Scope{}

// CheckScope returns ErrOutOfScope when userID is a paralegal and activityCode is outside the
// scope list. It returns nil while FlagParalegalScopeVerified is off.
// rule: BIL-L-046 (verify)
// A paralegal may only bill for work within the paralegal scope of practice.
func (s Scope) CheckScope(ctx context.Context, userID, activityCode string) error {
	if !flags.Enabled(ctx, FlagParalegalScopeVerified) {
		return nil
	}
	if s.Paralegals[userID] && !s.Activities[activityCode] {
		return fmt.Errorf("user %s activity %q: %w", userID, activityCode, ErrOutOfScope)
	}
	return nil
}

// FlagScope runs c on e and returns a copy of e carrying FlagOutOfScope when it is out of
// scope. The input's Flags slice is not changed.
// rule: BIL-L-046 (verify)
func FlagScope(ctx context.Context, c ScopeChecker, e TimeEntry) (TimeEntry, error) {
	err := c.CheckScope(ctx, e.UserID, e.ActivityCode)
	switch {
	case err == nil:
		return e, nil
	case !errors.Is(err, ErrOutOfScope):
		return e, fmt.Errorf("scope check entry %s: %w", e.ID, err)
	}
	if !slices.Contains(e.Flags, FlagOutOfScope) {
		e.Flags = append(slices.Clone(e.Flags), FlagOutOfScope)
	}
	return e, nil
}

// CheckBillable returns ErrOutOfScope for an entry flagged out of paralegal scope: it is
// blocked from billing until the flag is cleared.
// rule: BIL-L-046 (verify)
func CheckBillable(e TimeEntry) error {
	if slices.Contains(e.Flags, FlagOutOfScope) {
		return fmt.Errorf("bill entry %s: %w", e.ID, ErrOutOfScope)
	}
	return nil
}
