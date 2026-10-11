package payment

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// Allocation errors.
var (
	// ErrTrustReceipt is returned when trust money is allocated straight to a bill; fees
	// reach a bill from trust only through a trust fee withdrawal (409).
	ErrTrustReceipt = errors.New("payment: trust receipt cannot be allocated to a bill")
	// ErrNotDelivered is returned when client credit is applied to an undelivered bill.
	ErrNotDelivered = errors.New("payment: bill is not delivered")
	// ErrUnbalanced is returned when allocations, refunds and reversals do not sum to the
	// payment amount, or a row's buckets do not sum to its amount.
	ErrUnbalanced = errors.New("payment: allocations do not balance")
	// ErrRefundExceedsCredit is returned when a refund is larger than the client credit.
	ErrRefundExceedsCredit = errors.New("payment: refund exceeds client credit")
	// ErrPreparerApproves is returned when the requester approves their own refund.
	ErrPreparerApproves = errors.New("payment: requester cannot approve refund")
)

// Account is where money was deposited.
type Account string

// Accounts.
const (
	AccountOperating Account = "operating"
	AccountTrust     Account = "trust"
)

// Source says whose money is being allocated.
type Source string

// Sources.
const (
	SourcePayment      Source = "payment"
	SourceClientCredit Source = "client_credit"
)

// PreTaxOrder is how the pre-tax part of a principal allocation spreads over buckets.
type PreTaxOrder string

// Pre-tax orders (BIL-C-060).
const (
	PreTaxFeesFirst    PreTaxOrder = "fees_first"
	PreTaxProportional PreTaxOrder = "proportional"
)

// BillOrder is how a payment that names no bill spreads over the client's open bills.
type BillOrder string

// Bill orders (BIL-C-062).
const (
	BillOldestDueFirst BillOrder = "oldest_due_first"
	BillProportional   BillOrder = "proportional"
	BillManual         BillOrder = "manual"
)

// Buckets is an amount split by bucket: fees, taxable disbursements (own cost), non-taxable
// disbursements (agent), sales tax and interest. All int64 cents.
type Buckets struct {
	Fee      int64
	Dtx      int64
	Dnt      int64
	Tax      int64
	Interest int64
}

// Sum is the total of all five buckets.
func (b Buckets) Sum() int64 { return b.Fee + b.Dtx + b.Dnt + b.Tax + b.Interest }

// principal is the open pre-tax plus tax.
func (b Buckets) principal() int64 { return b.Fee + b.Dtx + b.Dnt + b.Tax }

// OpenBill is the open state of one bill as the allocator sees it.
type OpenBill struct {
	ID            string
	ClientID      string
	Number        string
	DueDate       time.Time
	DeliveredAt   time.Time // zero when undelivered
	Open          Buckets   // unpaid per bucket
	ReservedCents int64     // authorized or executed-unapplied trust fee transfers (BIL-C-054)
}

// capacity is what may still be allocated to the bill: balance minus reserved, never below 0.
func (b OpenBill) capacity() int64 {
	c := b.Open.Sum() - b.ReservedCents
	if c < 0 {
		return 0
	}
	return c
}

// Request is money to allocate. TargetBillID is set when the payment came through a bill
// link or names a bill number.
type Request struct {
	ClientID     string
	Source       Source
	DepositTo    Account
	AmountCents  int64
	TargetBillID string
	Bills        []OpenBill // the client's open bills
}

// Row is one allocation row. An empty BillID is unapplied money (client credit once the
// G-B3 gate approves); its buckets are all zero.
type Row struct {
	BillID      string
	AmountCents int64
	Buckets     Buckets
}

// Allocator computes allocation rows. The split is computed here; callers cannot set it.
type Allocator struct {
	Money     money.Arithmetic // nil means money.Default
	PreTax    PreTaxOrder
	BillOrder BillOrder
}

// Allocate spreads r over bills and returns rows that sum exactly to r.AmountCents.
func (a Allocator) Allocate(r Request) ([]Row, error) {
	if r.AmountCents <= 0 || r.ClientID == "" {
		return nil, fmt.Errorf("allocate: %w", ErrInvalid)
	}
	// rule: BIL-I-005
	// Bill allocations come only from operating payments.
	// rule: ARC-I-029
	// A trust-account payment is a trust receipt and may only target the trust ledger.
	if r.Source == SourcePayment && r.DepositTo != AccountOperating {
		return nil, fmt.Errorf("allocate to bill: %w", ErrTrustReceipt)
	}
	for _, b := range r.Bills {
		// rule: BIL-I-017
		// Client credit and payments are never applied across clients.
		if b.ClientID != r.ClientID {
			return nil, fmt.Errorf("allocate to bill %s: %w", b.ID, ErrOtherClient)
		}
	}
	bills, err := a.targets(r)
	if err != nil {
		return nil, err
	}
	shares, err := a.billShares(r.AmountCents, bills)
	if err != nil {
		return nil, err
	}
	var rows []Row
	left := r.AmountCents
	for i, b := range bills {
		if shares[i] == 0 {
			continue
		}
		bk, err := a.Split(shares[i], b.Open)
		if err != nil {
			return nil, fmt.Errorf("allocate to bill %s: %w", b.ID, err)
		}
		rows = append(rows, Row{BillID: b.ID, AmountCents: shares[i], Buckets: bk})
		left -= shares[i]
	}
	// rule: ARC-I-029
	// Any remainder is an explicit unapplied row so the rows sum to the payment amount.
	if left > 0 {
		rows = append(rows, Row{AmountCents: left})
	}
	if err := CheckRows(r.AmountCents, rows, nil); err != nil {
		return nil, err
	}
	return rows, nil
}

// targets returns the bills r may be allocated to, in allocation order.
func (a Allocator) targets(r Request) ([]OpenBill, error) {
	var out []OpenBill
	for _, b := range r.Bills {
		if r.TargetBillID != "" && b.ID != r.TargetBillID {
			continue
		}
		// rule: BIL-C-066
		// Client credit applies only to delivered bills of the same client.
		if r.Source == SourceClientCredit && b.DeliveredAt.IsZero() {
			if r.TargetBillID != "" {
				return nil, fmt.Errorf("apply credit to bill %s: %w", b.ID, ErrNotDelivered)
			}
			continue
		}
		out = append(out, b)
	}
	if r.TargetBillID != "" {
		if len(out) == 0 {
			return nil, fmt.Errorf("allocate to bill %s: %w", r.TargetBillID, ErrNotFound)
		}
		return out, nil
	}
	// rule: BIL-C-062
	// A payment naming a bill goes to that bill. Otherwise manual leaves it unapplied, and
	// the other orders take the client's open bills by (due date, delivered at, number).
	if a.BillOrder == BillManual {
		return nil, nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if !x.DueDate.Equal(y.DueDate) {
			return x.DueDate.Before(y.DueDate)
		}
		if !x.DeliveredAt.Equal(y.DeliveredAt) {
			return x.DeliveredAt.Before(y.DeliveredAt)
		}
		return x.Number < y.Number
	})
	return out, nil
}

// billShares returns how much of amount each bill takes.
func (a Allocator) billShares(amount int64, bills []OpenBill) ([]int64, error) {
	shares := make([]int64, len(bills))
	caps := make([]int64, len(bills))
	var total int64
	for i, b := range bills {
		// rule: BIL-C-054
		// Allocation to a bill is capped at balance minus reserved fee transfers; the excess
		// goes to unapplied.
		caps[i] = b.capacity()
		total += caps[i]
	}
	if total == 0 {
		return shares, nil
	}
	if a.BillOrder == BillProportional && amount < total {
		parts, err := a.arith().Allocate(money.Cents(amount), caps)
		if err != nil {
			return nil, fmt.Errorf("spread payment over bills: %w", err)
		}
		for i, p := range parts {
			shares[i] = min(int64(p), caps[i])
		}
		return shares, nil
	}
	left := amount
	for i := range bills {
		shares[i] = min(left, caps[i])
		left -= shares[i]
	}
	return shares, nil
}

// Split divides an allocation of p cents to one bill over its open buckets.
func (a Allocator) Split(p int64, open Buckets) (Buckets, error) {
	if p < 0 || p > open.Sum() {
		return Buckets{}, fmt.Errorf("split %d over open %d: %w", p, open.Sum(), ErrInvalid)
	}
	// rule: BIL-C-060
	// Principal (pre-tax + tax) before interest.
	po := open.principal()
	principal := min(p, po)
	out := Buckets{Interest: p - principal}
	// rule: BIL-C-202
	// Tax share = rhu(P * tax_open, principal_open); clearing principal takes the exact
	// remaining tax.
	// rule: BIL-T-082
	// The tax share depends only on the bill, never on the firm's pre-tax order.
	if principal == po {
		out.Tax = open.Tax
	} else if principal > 0 {
		t, err := a.arith().Rhu(principal*open.Tax, po)
		if err != nil {
			return Buckets{}, fmt.Errorf("tax share: %w", err)
		}
		out.Tax = t
	}
	pre := principal - out.Tax
	// rule: BIL-C-061
	// Fee bucket takes min(pre, fee_open), the rest to dtx, then dnt.
	if a.PreTax == PreTaxProportional && pre > 0 && pre < open.Fee+open.Dtx+open.Dnt {
		parts, err := a.arith().Allocate(money.Cents(pre), []int64{open.Fee, open.Dtx, open.Dnt})
		if err != nil {
			return Buckets{}, fmt.Errorf("pre-tax split: %w", err)
		}
		out.Fee, out.Dtx, out.Dnt = int64(parts[0]), int64(parts[1]), int64(parts[2])
		return out, nil
	}
	out.Fee = min(pre, open.Fee)
	pre -= out.Fee
	out.Dtx = min(pre, open.Dtx)
	out.Dnt = pre - out.Dtx
	return out, nil
}

// CheckRows verifies a payment's rows. bills, when given, are the open bills the rows were
// allocated against, keyed by id.
func CheckRows(amount int64, rows []Row, bills map[string]OpenBill) error {
	var sum int64
	used := map[string]Buckets{}
	for _, r := range rows {
		sum += r.AmountCents
		// rule: BIL-I-026
		// Bill rows: buckets sum to the amount. Non-bill rows: all five buckets are zero.
		if r.BillID == "" {
			if r.Buckets != (Buckets{}) {
				return fmt.Errorf("unapplied row with buckets: %w", ErrUnbalanced)
			}
			continue
		}
		if r.Buckets.Sum() != r.AmountCents {
			return fmt.Errorf("row for bill %s: %w", r.BillID, ErrUnbalanced)
		}
		u := used[r.BillID]
		u.Fee += r.Buckets.Fee
		u.Dtx += r.Buckets.Dtx
		u.Dnt += r.Buckets.Dnt
		u.Tax += r.Buckets.Tax
		u.Interest += r.Buckets.Interest
		used[r.BillID] = u
	}
	for id, u := range used {
		b, ok := bills[id]
		if !ok {
			continue
		}
		// rule: BIL-I-026
		// Per bill and bucket, allocations never exceed what is open.
		o := b.Open
		if u.Fee > o.Fee || u.Dtx > o.Dtx || u.Dnt > o.Dnt || u.Tax > o.Tax || u.Interest > o.Interest {
			return fmt.Errorf("bill %s bucket over-allocated: %w", id, ErrUnbalanced)
		}
	}
	// rule: ARC-I-029
	// Allocations of one payment sum exactly to its amount.
	if sum != amount {
		return fmt.Errorf("rows sum %d, payment %d: %w", sum, amount, ErrUnbalanced)
	}
	return nil
}

// CheckBalance verifies that a payment is fully accounted for.
func CheckBalance(amount int64, rows []Row, refundCents, reversalCents int64) error {
	var sum int64
	for _, r := range rows {
		sum += r.AmountCents
	}
	// rule: BIL-I-012
	// sum(allocations incl. client credit) + refunds + reversals = amount.
	if sum+refundCents+reversalCents != amount {
		return fmt.Errorf("payment of %d accounts for %d: %w", amount, sum+refundCents+reversalCents, ErrUnbalanced)
	}
	return nil
}

// Refund is a request to refund client credit.
type Refund struct {
	ClientID          string
	AmountCents       int64
	CreditCents       int64 // the client's client-credit balance
	OriginalCaptured  int64 // captured on the original method; 0 if none
	RequestedBy       string
	ApprovedBy        string
	ApproverIsLawyer  bool
	ApproverIsAgent   bool
	OriginalMethodRef string
}

// RefundRoute says how a refund is paid.
type RefundRoute string

// Refund routes.
const (
	RefundOriginalMethod RefundRoute = "original_method"
	RefundOperating      RefundRoute = "operating_cheque_or_eft"
)

// CheckRefund validates a client-credit refund and returns its route.
func CheckRefund(r Refund) (RefundRoute, error) {
	if r.AmountCents <= 0 {
		return "", fmt.Errorf("refund: %w", ErrInvalid)
	}
	// rule: BIL-C-064
	// Refund <= client credit; to the original method up to its captured amount, else from
	// operating; approved under G-B6 by a lawyer who is not the requester.
	if r.AmountCents > r.CreditCents {
		return "", fmt.Errorf("refund: %w", ErrRefundExceedsCredit)
	}
	if r.ApprovedBy == "" || r.ApprovedBy == r.RequestedBy || r.ApproverIsAgent || !r.ApproverIsLawyer {
		return "", fmt.Errorf("refund approval: %w", ErrPreparerApproves)
	}
	if r.OriginalMethodRef != "" && r.AmountCents <= r.OriginalCaptured {
		return RefundOriginalMethod, nil
	}
	return RefundOperating, nil
}

// DefaultCreditMaxBusinessDays is the default client_credit_max_business_days.
const DefaultCreditMaxBusinessDays = 5

// CreditOverdue reports whether client credit created on createdOn and still held on today
// is past the firm limit. businessDays counts business days from one date to another.
func CreditOverdue(createdOn, today time.Time, maxDays int, businessDays func(from, to time.Time) int) bool {
	if maxDays <= 0 {
		maxDays = DefaultCreditMaxBusinessDays
	}
	// rule: BIL-C-067
	// Client credit is client money in operating: past the limit the aging job opens a
	// trust exception.
	return businessDays(createdOn, today) > maxDays
}

// arith returns Money, or money.Default when Money is nil.
func (a Allocator) arith() money.Arithmetic {
	if a.Money == nil {
		return money.Default
	}
	return a.Money
}
