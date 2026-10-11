package batch

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/kernel/clock"
)

// ErrInvalid is returned when a run has no id or a target has no matter or client.
var ErrInvalid = errors.New("batch: invalid run")

// Target is one matter (or client, for client-level billing) a batch run may bill.
// Exactly one of MatterID and ClientID is set.
type Target struct {
	MatterID      string
	ClientID      string
	UnbilledCents int64 // approved WIP up to the cut-off
}

// subject is the matter id, or the client id for client-level billing.
func (t Target) subject() string {
	if t.MatterID != "" {
		return "matter:" + t.MatterID
	}
	return "client:" + t.ClientID
}

// Run is one batch billing run.
type Run struct {
	ID               string // batch_run_id
	PeriodTo         clock.Date
	MinimumBillCents int64
	Targets          []Target
}

// Drafts creates draft bills. Create builds one draft in its own transaction and stores it
// under key; a key already stored returns the first draft id with created=false. Drafts go
// to the bill approval gate; Drafts never approves.
type Drafts interface {
	Create(ctx context.Context, key string, t Target, periodTo clock.Date) (draftID string, created bool, err error)
}

// Failure is a target whose draft could not be built.
type Failure struct {
	Target Target
	Err    error
}

// Result reports what a run did.
type Result struct {
	Created  []string // draft ids built by this call
	Existing []string // draft ids an earlier call of the same run already built
	Skipped  []Target // under the minimum bill
	Failed   []Failure
}

// Key is the idempotency key of one draft: (batch_run_id, matter_id | client_id, period_to).
//
// rule: BIL-C-110
func Key(runID string, t Target, periodTo clock.Date) string {
	return fmt.Sprintf("%s|%s|%04d-%02d-%02d", runID, t.subject(), periodTo.Year, int(periodTo.Month), periodTo.Day)
}

// Execute builds one draft per eligible target. Each draft is built on its own, so one
// failing target does not undo the others; rerunning the same run creates no new drafts.
func Execute(ctx context.Context, d Drafts, run Run) (Result, error) {
	var res Result
	if run.ID == "" {
		return res, fmt.Errorf("batch run id: %w", ErrInvalid)
	}
	for _, t := range run.Targets {
		if (t.MatterID == "") == (t.ClientID == "") {
			return res, fmt.Errorf("batch target needs one of matter or client: %w", ErrInvalid)
		}
	}
	for _, t := range run.Targets {
		// rule: BIL-C-110
		if t.UnbilledCents < run.MinimumBillCents {
			res.Skipped = append(res.Skipped, t)
			continue
		}
		// rule: BIL-C-110
		id, created, err := d.Create(ctx, Key(run.ID, t, run.PeriodTo), t, run.PeriodTo)
		switch {
		case err != nil:
			res.Failed = append(res.Failed, Failure{Target: t, Err: fmt.Errorf("batch draft %s: %w", t.subject(), err)})
		case created:
			res.Created = append(res.Created, id)
		default:
			res.Existing = append(res.Existing, id)
		}
	}
	return res, nil
}
