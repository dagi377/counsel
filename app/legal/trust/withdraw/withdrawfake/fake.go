// Package withdrawfake is an in-memory withdraw.Service for tests in other packages.
package withdrawfake

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/gate"
)

var _ withdraw.Service = (*Fake)(nil)

type ledgerKey struct{ account, matter string }

type ledger struct {
	cleared   int64
	uncleared int64
}

// Fake keeps matter ledgers and withdrawals in memory.
type Fake struct {
	mu      sync.Mutex
	ttl     time.Duration
	ledgers map[ledgerKey]*ledger
	byID    map[string]*withdraw.Withdrawal
	byKey   map[string]string
	seq     int
	guard   testing.TB
}

// GuardEffects makes the effect-owned methods (Authorize: withdrawal.status=authorized,
// gate trust.withdrawal) fail tb and refuse when called outside a gate effect, so a handler
// test proves its route never reaches an effect-owned write.
func (f *Fake) GuardEffects(tb testing.TB) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.guard = tb
}

func (f *Fake) effectOnly(ctx context.Context, method string) error {
	f.mu.Lock()
	tb := f.guard
	f.mu.Unlock()
	if tb == nil {
		return nil
	}
	// rule: ARC-I-012
	if err := gate.RequireEffect(ctx); err != nil {
		tb.Helper()
		tb.Errorf("withdrawfake: effect-owned %s called outside a gate effect", method)
		return fmt.Errorf("withdrawfake: %s: %w", method, err)
	}
	return nil
}

// New returns an empty Fake whose authorizations expire after ttl.
func New(ttl time.Duration) *Fake {
	return &Fake{ttl: ttl, ledgers: map[ledgerKey]*ledger{}, byID: map[string]*withdraw.Withdrawal{}, byKey: map[string]string{}}
}

// SetBalance sets a matter's cleared and uncleared trust balance.
func (f *Fake) SetBalance(account, matter string, cleared, uncleared int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ledgers[ledgerKey{account, matter}] = &ledger{cleared: cleared, uncleared: uncleared}
}

func (f *Fake) ledger(account, matter string) *ledger {
	l, ok := f.ledgers[ledgerKey{account, matter}]
	if !ok {
		l = &ledger{}
		f.ledgers[ledgerKey{account, matter}] = l
	}
	return l
}

// available is cleared minus authorized-not-executed; the caller holds mu.
func (f *Fake) available(account, matter string) int64 {
	avail := f.ledger(account, matter).cleared
	for _, w := range f.byID {
		if w.Status == withdraw.StatusAuthorized && w.Request.TrustAccountID == account && w.Request.MatterID == matter {
			avail -= w.Request.AmountCents
		}
	}
	return avail
}

// Request validates and records r.
func (f *Fake) Request(_ context.Context, r withdraw.Request) (withdraw.Withdrawal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byKey[r.IdempotencyKey]; ok && r.IdempotencyKey != "" {
		return *f.byID[id], nil
	}
	switch p := strings.ToLower(strings.TrimSpace(r.Payee)); {
	case p == "cash" || p == "bearer":
		return withdraw.Withdrawal{}, withdraw.ErrBearerPayee
	case p == "" || r.AmountCents <= 0 || r.MatterID == "" || r.TrustAccountID == "" || r.PreparedBy == "":
		return withdraw.Withdrawal{}, withdraw.ErrInvalid
	}
	if r.Method == withdraw.MethodEFT && r.EFTAuthorizationID == "" {
		return withdraw.Withdrawal{}, withdraw.ErrNoEFTAuthorization
	}
	avail := f.available(r.TrustAccountID, r.MatterID)
	if r.AmountCents > avail {
		if r.AmountCents <= avail+f.ledger(r.TrustAccountID, r.MatterID).uncleared {
			return withdraw.Withdrawal{}, withdraw.ErrUnclearedFunds
		}
		return withdraw.Withdrawal{}, withdraw.ErrTrustOverdraw
	}
	f.seq++
	w := &withdraw.Withdrawal{ID: "wd_" + strconv.Itoa(f.seq), Request: r, Status: withdraw.StatusRequested, GateID: "gate_" + strconv.Itoa(f.seq)}
	f.byID[w.ID] = w
	if r.IdempotencyKey != "" {
		f.byKey[r.IdempotencyKey] = w.ID
	}
	return *w, nil
}

// Authorize re-checks the available balance and authorizes the withdrawal.
func (f *Fake) Authorize(ctx context.Context, id, by string, at time.Time) (withdraw.Withdrawal, error) {
	if err := f.effectOnly(ctx, "Authorize"); err != nil {
		return withdraw.Withdrawal{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.byID[id]
	if !ok {
		return withdraw.Withdrawal{}, fmt.Errorf("withdrawfake: %s: %w", id, withdraw.ErrNotFound)
	}
	if w.Status != withdraw.StatusRequested {
		return *w, nil
	}
	if by == w.Request.PreparedBy {
		return withdraw.Withdrawal{}, withdraw.ErrPreparerApproves
	}
	if w.Request.AmountCents > f.available(w.Request.TrustAccountID, w.Request.MatterID) {
		return withdraw.Withdrawal{}, withdraw.ErrTrustOverdraw
	}
	w.Status, w.AuthorizedBy, w.AuthorizedAt, w.ExpiresAt = withdraw.StatusAuthorized, by, at, at.Add(f.ttl)
	return *w, nil
}

// RecordExecution posts the withdrawal against the matter ledger.
func (f *Fake) RecordExecution(_ context.Context, id, by string, executedOn time.Time) (withdraw.Withdrawal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.byID[id]
	if !ok {
		return withdraw.Withdrawal{}, fmt.Errorf("withdrawfake: %s: %w", id, withdraw.ErrNotFound)
	}
	if w.Status != withdraw.StatusAuthorized || by == w.AuthorizedBy || !executedOn.Before(w.ExpiresAt) {
		return withdraw.Withdrawal{}, withdraw.ErrNotAuthorized
	}
	f.ledger(w.Request.TrustAccountID, w.Request.MatterID).cleared -= w.Request.AmountCents
	w.Status, w.ExecutedBy, w.ExecutedOn = withdraw.StatusExecuted, by, executedOn
	return *w, nil
}

// Get returns one withdrawal.
func (f *Fake) Get(_ context.Context, id string) (withdraw.Withdrawal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.byID[id]
	if !ok {
		return withdraw.Withdrawal{}, fmt.Errorf("withdrawfake: %s: %w", id, withdraw.ErrNotFound)
	}
	return *w, nil
}

// Available returns cleared minus authorized-not-executed.
func (f *Fake) Available(_ context.Context, account, matter string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available(account, matter), nil
}
