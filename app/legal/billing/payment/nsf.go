package payment

import "fmt"

// Reversal is a reversed payment: one row per original allocation row with the buckets
// negated, and the bills' open buckets after the reversal.
type Reversal struct {
	Reason ReversalReason
	Rows   []Row
	// Open is each reversed bill's open buckets after the reversal, keyed by bill id.
	Open map[string]Buckets
	// UnappliedCents is the unapplied money (client credit) the reversal takes back.
	UnappliedCents int64
}

// ReverseRows reverses a captured payment's allocation rows, as for an NSF cheque, a
// failed pre-authorized debit or a chargeback. open is the bills' open buckets before the
// reversal; it is not modified.
func ReverseRows(amount int64, reason ReversalReason, rows []Row, open map[string]Buckets) (Reversal, error) {
	switch reason {
	case ReasonNSF, ReasonRefund, ReasonChargeback:
	default:
		return Reversal{}, fmt.Errorf("reverse payment: reason %q: %w", reason, ErrInvalid)
	}
	if err := CheckRows(amount, rows, nil); err != nil {
		return Reversal{}, fmt.Errorf("reverse payment: %w", err)
	}
	out := Reversal{Reason: reason, Open: map[string]Buckets{}}
	for _, r := range rows {
		if r.BillID == "" {
			out.UnappliedCents += r.AmountCents
			out.Rows = append(out.Rows, Row{AmountCents: -r.AmountCents})
			continue
		}
		o, ok := out.Open[r.BillID]
		if !ok {
			if o, ok = open[r.BillID]; !ok {
				return Reversal{}, fmt.Errorf("reverse payment: bill %s: %w", r.BillID, ErrNotFound)
			}
		}
		// rule: BIL-C-065
		// Every allocation is reversed with its stored bucket split, so each bucket of the
		// bill reopens by exactly what the payment took from it.
		b := r.Buckets
		out.Rows = append(out.Rows, Row{BillID: r.BillID, AmountCents: -r.AmountCents, Buckets: Buckets{
			Fee: -b.Fee, Dtx: -b.Dtx, Dnt: -b.Dnt, Tax: -b.Tax, Interest: -b.Interest,
		}})
		o.Fee += b.Fee
		o.Dtx += b.Dtx
		o.Dnt += b.Dnt
		o.Tax += b.Tax
		o.Interest += b.Interest
		out.Open[r.BillID] = o
	}
	return out, nil
}
