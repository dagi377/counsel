// Package ledgerfake is an in-memory kernel/ledger for tests: no network, no Postgres.
package ledgerfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/ledger"
)

var _ ledger.Ledger = (*Fake)(nil)

type stored struct {
	id         ledger.EntryID
	e          ledger.Entry
	reversedBy ledger.EntryID
}

// Fake keeps entries in memory. The tx argument is ignored.
type Fake struct {
	mu       sync.Mutex
	entries  []stored
	bySource map[string]ledger.EntryID
	closed   map[string][]ledger.Period
	accounts map[string]bool
	// Today dates reversals; tests set it.
	Today ledger.Date
}

// New returns an empty ledger. When accounts is non-empty, lines must use one of them.
func New(today ledger.Date, accounts ...string) *Fake {
	f := &Fake{bySource: map[string]ledger.EntryID{}, closed: map[string][]ledger.Period{}, accounts: map[string]bool{}, Today: today}
	for _, a := range accounts {
		f.accounts[a] = true
	}
	return f
}

func (f *Fake) validate(e ledger.Entry) error {
	if e.Book == "" || e.SourceKind == "" || e.SourceID == "" || len(e.Lines) < 2 {
		return fmt.Errorf("entry %s/%s: %w", e.SourceKind, e.SourceID, ledger.ErrInvalidEntry)
	}
	var dr, cr ledger.Cents
	for i, l := range e.Lines {
		if l.Amount <= 0 {
			return fmt.Errorf("line %d amount: %w", i, ledger.ErrInvalidEntry)
		}
		if len(f.accounts) > 0 && !f.accounts[l.Account] {
			return fmt.Errorf("line %d account %q: %w", i, l.Account, ledger.ErrUnknownAccount)
		}
		switch l.Side {
		case ledger.Debit:
			dr += l.Amount
		case ledger.Credit:
			cr += l.Amount
		default:
			return fmt.Errorf("line %d side %q: %w", i, l.Side, ledger.ErrInvalidEntry)
		}
	}
	if dr != cr {
		return fmt.Errorf("entry %s/%s: %w", e.SourceKind, e.SourceID, ledger.ErrUnbalanced)
	}
	for _, p := range f.closed[e.Book] {
		if !e.Date.Before(p.From) && !p.To.Before(e.Date) {
			return fmt.Errorf("entry %s/%s: %w", e.SourceKind, e.SourceID, ledger.ErrPeriodClosed)
		}
	}
	return nil
}

func (f *Fake) post(e ledger.Entry) (ledger.EntryID, error) {
	key := e.SourceKind + "\x00" + e.SourceID
	if id, ok := f.bySource[key]; ok {
		return id, nil
	}
	if err := f.validate(e); err != nil {
		return "", err
	}
	id := ledger.EntryID("je-" + strconv.Itoa(len(f.entries)+1))
	e.Lines = append([]ledger.Line(nil), e.Lines...)
	f.entries = append(f.entries, stored{id: id, e: e})
	f.bySource[key] = id
	return id, nil
}

// Post validates and stores e, idempotent per (SourceKind, SourceID).
func (f *Fake) Post(_ context.Context, _ pgx.Tx, e ledger.Entry) (ledger.EntryID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.post(e)
}

// Reverse posts the mirror entry dated f.Today.
func (f *Fake) Reverse(_ context.Context, _ pgx.Tx, id ledger.EntryID, reason string) (ledger.EntryID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.find(id)
	if i < 0 {
		return "", fmt.Errorf("reverse %s: %w", id, ledger.ErrNotFound)
	}
	if f.entries[i].reversedBy != "" {
		return "", fmt.Errorf("reverse %s: %w", id, ledger.ErrAlreadyReversed)
	}
	orig := f.entries[i].e
	rev := ledger.Entry{Book: orig.Book, Date: f.Today, SourceKind: "reversal", SourceID: string(id), Memo: reason}
	for _, l := range orig.Lines {
		l.Side = map[ledger.Side]ledger.Side{ledger.Debit: ledger.Credit, ledger.Credit: ledger.Debit}[l.Side]
		rev.Lines = append(rev.Lines, l)
	}
	rid, err := f.post(rev)
	if err != nil {
		return "", err
	}
	f.entries[i].reversedBy = rid
	return rid, nil
}

// ClosePeriod records p as closed for book.
func (f *Fake) ClosePeriod(_ context.Context, _ pgx.Tx, book string, p ledger.Period) error {
	if book == "" || p.To.Before(p.From) {
		return fmt.Errorf("close period: %w", ledger.ErrInvalidEntry)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed[book] = append(f.closed[book], p)
	return nil
}

// Balance sums debits minus credits on account up to asOf.
func (f *Fake) Balance(_ context.Context, account string, asOf ledger.Date) (ledger.Cents, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b ledger.Cents
	for _, s := range f.entries {
		if asOf.Before(s.e.Date) {
			continue
		}
		for _, l := range s.e.Lines {
			if l.Account != account {
				continue
			}
			if l.Side == ledger.Debit {
				b += l.Amount
			} else {
				b -= l.Amount
			}
		}
	}
	return b, nil
}

// Entry returns a stored entry by id.
func (f *Fake) Entry(id ledger.EntryID) (ledger.Entry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.find(id); i >= 0 {
		return f.entries[i].e, true
	}
	return ledger.Entry{}, false
}

func (f *Fake) find(id ledger.EntryID) int {
	for i, s := range f.entries {
		if s.id == id {
			return i
		}
	}
	return -1
}
