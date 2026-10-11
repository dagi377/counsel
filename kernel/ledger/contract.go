// CONTRACT: kernel/ledger (T-010). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html ("kernel/ledger": Post, Reverse,
// ClosePeriod, Balance). Bodies live in the T-010 subtasks (post.go, reverse.go, period.go,
// chart.go, balance.go). The engine knows no account code: accounts, books and posting
// templates are app data registered at boot.

package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Cents is an amount in integer cents. It has the shape of money.Cents and becomes an alias
// of it once kernel/money (T-003) is on main.
type Cents int64

// Date is a civil date in the business time zone. It becomes an alias of the kernel/clock
// date type once kernel/clock (T-004) is on main.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Before reports whether d is strictly before o.
func (d Date) Before(o Date) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}

// Subject is an opaque reference to a business object (registry.Subject shape).
type Subject struct {
	Kind string
	ID   string
}

// EntryID identifies one posted journal entry.
type EntryID string

// Side is the side of a journal line.
type Side string

// Sides.
const (
	Debit  Side = "debit"
	Credit Side = "credit"
)

// Line is one journal line. Amount is always positive; Side says which way it moves.
type Line struct {
	Account string
	Side    Side
	Amount  Cents
	Subject Subject // optional: the business object the line is about
}

// Entry is a balanced journal entry: debits equal credits. It is idempotent per
// (SourceKind, SourceID): posting the same source twice returns the first EntryID.
type Entry struct {
	Book       string
	Date       Date
	SourceKind string
	SourceID   string
	Memo       string
	Lines      []Line
}

// Period is an inclusive date range of one book, e.g. a month.
type Period struct {
	From Date
	To   Date
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrUnbalanced means total debits differ from total credits.
	ErrUnbalanced = errors.New("ledger: entry is not balanced")
	// ErrInvalidEntry means the entry has no lines, no book, no source, a bad side or a
	// non-positive amount.
	ErrInvalidEntry = errors.New("ledger: invalid entry")
	// ErrUnknownAccount means a line names an account the chart does not register.
	ErrUnknownAccount = errors.New("ledger: unknown account")
	// ErrPeriodClosed means the entry is dated inside a closed period; post the correction
	// in the current period instead.
	ErrPeriodClosed = errors.New("ledger: period closed")
	// ErrNotFound means the entry does not exist.
	ErrNotFound = errors.New("ledger: entry not found")
	// ErrAlreadyReversed means the entry already has a reversal.
	ErrAlreadyReversed = errors.New("ledger: entry already reversed")
)

// Ledger posts balanced double-entry journals. Rows are never updated or deleted;
// corrections are reversing entries.
type Ledger interface {
	// Post writes a balanced entry inside tx and returns its id.
	Post(ctx context.Context, tx pgx.Tx, e Entry) (EntryID, error)
	// Reverse posts the mirror of id (same lines, sides swapped) dated today, or in the
	// current period when the original period is closed.
	Reverse(ctx context.Context, tx pgx.Tx, id EntryID, reason string) (EntryID, error)
	// ClosePeriod refuses further postings into p for book.
	ClosePeriod(ctx context.Context, tx pgx.Tx, book string, p Period) error
	// Balance returns debits minus credits on account for entries dated on or before asOf.
	Balance(ctx context.Context, account string, asOf Date) (Cents, error)
}
