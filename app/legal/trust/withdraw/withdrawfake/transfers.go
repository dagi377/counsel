package withdrawfake

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// TransferRow is one in-memory trust.trust_transfer row.
type TransferRow struct {
	ID          string
	From, To    trust.LedgerID
	AmountCents int64
	Status      string
	GateID      string
	RequestedBy string
	Reason      string
}

// Transfers is an in-memory transfer store: withdraw.TransferRequestStore, withdraw.TransferStore
// and withdraw.TransferGateStore over client ledgers, plus a TxRunner whose tx rolls back on
// error. A tx is nil.
type Transfers struct {
	mu      sync.Mutex
	ledgers map[ledgerKey]trust.Ledger
	posted  map[trust.LedgerID]int64
	rows    map[string]TransferRow
	legs    []trust.Txn
	n       int
}

var (
	_ withdraw.TransferRequestStore = (*Transfers)(nil)
	_ withdraw.TransferStore        = (*Transfers)(nil)
	_ withdraw.TransferGateStore    = (*Transfers)(nil)
	_ withdraw.TxRunner             = (*Transfers)(nil)
)

// NewTransfers returns an empty store.
func NewTransfers() *Transfers {
	return &Transfers{ledgers: map[ledgerKey]trust.Ledger{}, posted: map[trust.LedgerID]int64{}, rows: map[string]TransferRow{}}
}

// AddLedger opens the matter's ledger on account holding cents, cleared, and returns its id.
func (f *Transfers) AddLedger(account, matter string, cents int64) trust.LedgerID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := trust.LedgerID(fmt.Sprintf("ledger-%s-%s", account, matter))
	f.ledgers[ledgerKey{account, matter}] = trust.Ledger{ID: id, AccountID: trust.AccountID(account), MatterID: matter}
	f.posted[id] = cents
	return id
}

// Adjust moves a ledger's balance by cents, as a receipt or a disbursement would.
func (f *Transfers) Adjust(id trust.LedgerID, cents int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted[id] += cents
}

// Balance is the ledger's posted balance in cents.
func (f *Transfers) Balance(id trust.LedgerID) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.posted[id]
}

// Row returns transfer id.
func (f *Transfers) Row(id string) (TransferRow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	return r, ok
}

// Rows returns every transfer, in id order.
func (f *Transfers) Rows() []TransferRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := slices.Sorted(maps.Keys(f.rows))
	out := make([]TransferRow, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.rows[id])
	}
	return out
}

// Legs returns the posted transfer lines.
func (f *Transfers) Legs() []trust.Txn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.legs)
}

// InTx implements withdraw.TxRunner: fn runs with a nil tx, and an error restores the store
// as it was before fn.
func (f *Transfers) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	f.mu.Lock()
	posted, rows, legs, n := maps.Clone(f.posted), maps.Clone(f.rows), slices.Clone(f.legs), f.n
	f.mu.Unlock()
	if err := fn(ctx, nil); err != nil {
		f.mu.Lock()
		f.posted, f.rows, f.legs, f.n = posted, rows, legs, n
		f.mu.Unlock()
		return err
	}
	return nil
}

// LedgerOf implements withdraw.TransferRequestStore.
func (f *Transfers) LedgerOf(_ context.Context, _ pgx.Tx, account, matter string) (trust.LedgerID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.ledgers[ledgerKey{account, matter}]
	if !ok {
		return "", withdraw.ErrNotFound
	}
	return l.ID, nil
}

// LockLedgers implements withdraw.TransferStore.
func (f *Transfers) LockLedgers(_ context.Context, _ pgx.Tx, a, b trust.LedgerID) ([]trust.Ledger, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []trust.Ledger
	for _, l := range f.ledgers {
		if l.ID == a || l.ID == b {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(x, y trust.Ledger) int {
		switch {
		case x.ID < y.ID:
			return -1
		case x.ID > y.ID:
			return 1
		}
		return 0
	})
	return out, nil
}

// Available implements withdraw.TransferStore: posted cents less authorised transfers out.
func (f *Transfers) Available(_ context.Context, _ pgx.Tx, id trust.LedgerID, except string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	avail := f.posted[id]
	for _, r := range f.rows {
		if r.From == id && r.Status == "authorized" && r.ID != except {
			avail -= r.AmountCents
		}
	}
	return avail, nil
}

// InsertTransfer implements withdraw.TransferRequestStore.
func (f *Transfers) InsertTransfer(_ context.Context, _ pgx.Tx, from, to trust.LedgerID, r withdraw.TransferRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id := fmt.Sprintf("transfer-%d", f.n)
	f.rows[id] = TransferRow{ID: id, From: from, To: to, AmountCents: r.AmountCents, Status: "pending_authorization",
		RequestedBy: r.PreparedBy, Reason: r.Reason}
	return id, nil
}

// RecordGate implements withdraw.TransferRequestStore.
func (f *Transfers) RecordGate(_ context.Context, _ pgx.Tx, id, gateID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.Status != "pending_authorization" {
		return withdraw.ErrNotFound
	}
	r.GateID = gateID
	f.rows[id] = r
	return nil
}

// LockTransfer implements withdraw.TransferGateStore.
func (f *Transfers) LockTransfer(_ context.Context, _ pgx.Tx, id string) (withdraw.HeldTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return withdraw.HeldTransfer{}, withdraw.ErrNotFound
	}
	h := withdraw.HeldTransfer{ID: r.ID, FromLedger: r.From, ToLedger: r.To, AmountCents: r.AmountCents, Status: r.Status}
	for _, l := range f.ledgers {
		switch l.ID {
		case r.From:
			h.AccountID, h.FromMatterID = l.AccountID, l.MatterID
		case r.To:
			h.ToMatterID = l.MatterID
		}
	}
	return h, nil
}

func (f *Transfers) move(id, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok || r.Status != from {
		return withdraw.ErrNotAuthorized
	}
	r.Status = to
	f.rows[id] = r
	return nil
}

// MarkAuthorized implements withdraw.TransferGateStore.
func (f *Transfers) MarkAuthorized(_ context.Context, _ pgx.Tx, id, _ string) error {
	return f.move(id, "pending_authorization", "authorized")
}

// MarkPosted implements withdraw.TransferStore.
func (f *Transfers) MarkPosted(_ context.Context, _ pgx.Tx, id string) error {
	return f.move(id, "authorized", "posted")
}

// InsertLeg implements withdraw.TransferStore: the line moves its ledger's balance.
func (f *Transfers) InsertLeg(_ context.Context, _ pgx.Tx, _ string, t trust.Txn) (trust.TxnID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.legs = append(f.legs, t)
	f.posted[t.LedgerID] += t.AmountCents
	return trust.TxnID(fmt.Sprintf("txn-%d", len(f.legs))), nil
}

// Requests returns the transfer request service over f, opening gates with g.
func (f *Transfers) Requests(g withdraw.Gates) withdraw.TransferRequests {
	return withdraw.TransferRequests{DB: f, Store: f, Gates: g}
}
