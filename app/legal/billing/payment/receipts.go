package payment

import (
	"fmt"
	"sync"
	"time"
)

// AccountAR is the operating ledger account a bill payment credits.
const AccountAR = "OP-1100"

// receiptCreditAccount is the operating account holding unapplied money (client credit).
const receiptCreditAccount = "OP-2200"

// ReceiptLine is one part of a receipt: what it paid and where it was posted. An empty
// BillID is unapplied money, held for the client in client credit.
type ReceiptLine struct {
	BillID      string
	AmountCents int64
	Account     string
}

// Receipt is one entry in the general (operating) receipts journal.
type Receipt struct {
	PaymentID   string
	ClientID    string
	ReceivedOn  time.Time // business date, America/Toronto
	AmountCents int64
	Lines       []ReceiptLine
}

// ReceiptsJournal is the general receipts journal: every operating payment, once, with the
// bills it pays. It is safe for concurrent use.
type ReceiptsJournal struct {
	mu      sync.Mutex
	entries []Receipt
	byID    map[string]int
}

// Record adds payment p with its allocations to the journal. Recording a payment again
// returns the first entry.
func (j *ReceiptsJournal) Record(p Payment, allocs []Allocation) (Receipt, error) {
	c := p.Capture
	// rule: BIL-I-016
	// Money for fees not yet earned never enters operating: a retainer is refused here.
	if c.Kind == KindRetainer {
		return Receipt{}, fmt.Errorf("record receipt %s: %w", p.ID, ErrRetainerToOperating)
	}
	if p.ID == "" || c.ClientID == "" || c.AmountCents <= 0 {
		return Receipt{}, fmt.Errorf("record receipt: %w", ErrInvalid)
	}
	if p.Status == StatusReversed {
		return Receipt{}, fmt.Errorf("record receipt %s: %w", p.ID, ErrReversed)
	}
	r := Receipt{PaymentID: p.ID, ClientID: c.ClientID, ReceivedOn: c.ReceivedOn, AmountCents: c.AmountCents}
	var sum int64
	for _, a := range allocs {
		if a.PaymentID != p.ID {
			return Receipt{}, fmt.Errorf("record receipt %s: allocation of %s: %w", p.ID, a.PaymentID, ErrInvalid)
		}
		// rule: BIL-L-042 (verify)
		// Each payment is applied to the bill it pays; the remainder is shown as client
		// credit, never silently absorbed.
		acct := AccountAR
		if a.BillID == "" {
			acct = receiptCreditAccount
		}
		r.Lines = append(r.Lines, ReceiptLine{BillID: a.BillID, AmountCents: a.Total(), Account: acct})
		sum += a.Total()
	}
	if sum != c.AmountCents {
		return Receipt{}, fmt.Errorf("record receipt %s: lines %d, payment %d: %w", p.ID, sum, c.AmountCents, ErrUnbalanced)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if i, ok := j.byID[p.ID]; ok {
		return cloneReceipt(j.entries[i]), nil
	}
	if j.byID == nil {
		j.byID = map[string]int{}
	}
	j.byID[p.ID] = len(j.entries)
	j.entries = append(j.entries, r)
	return cloneReceipt(r), nil
}

// Entries returns the journal in recording order.
func (j *ReceiptsJournal) Entries() []Receipt {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Receipt, len(j.entries))
	for i, r := range j.entries {
		out[i] = cloneReceipt(r)
	}
	return out
}

// PaidTo returns the cents the journal records against one bill.
func (j *ReceiptsJournal) PaidTo(billID string) int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	var n int64
	for _, r := range j.entries {
		for _, l := range r.Lines {
			if l.BillID == billID && billID != "" {
				n += l.AmountCents
			}
		}
	}
	return n
}

func cloneReceipt(r Receipt) Receipt {
	r.Lines = append([]ReceiptLine(nil), r.Lines...)
	return r
}
