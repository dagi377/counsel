package trustapply

import (
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

// IssuedBill is the slice of an issued bill a retainer application reads for tax.
type IssuedBill struct {
	ID       string
	IssuedOn time.Time // bill date (approval date), America/Toronto; zero when not issued
	TaxCents int64     // tax already computed on the bill
}

// TaxPoint is the tax a trust movement makes payable, reported via the bill it names.
type TaxPoint struct {
	BillID   string
	On       time.Time // business date tax becomes payable
	TaxCents int64
}

// Retainer decides the tax timing of retainers held in trust.
type Retainer struct {
	Flags flags.Flags
}

// OnReceipt returns the tax points a retainer received into trust creates: none.
func (r Retainer) OnReceipt(amountCents int64) ([]TaxPoint, error) {
	if !r.Flags.Enabled(FlagBylaw9Verified) {
		return nil, ErrDisabled
	}
	if amountCents <= 0 {
		return nil, fmt.Errorf("retainer receipt: %w", ErrInvalid)
	}
	// rule: BIL-T-051 (verify)
	// A retainer in trust is a deposit, not consideration: its receipt posts no tax.
	return nil, nil
}

// OnApply returns the tax point when amountCents of a retainer is applied on appliedOn to
// bill b. The tax is the bill's own tax, unchanged by the application, and is payable on
// the earlier of the bill date and the application date (BIL-T-050).
func (r Retainer) OnApply(b IssuedBill, appliedOn time.Time, amountCents int64) (TaxPoint, error) {
	if !r.Flags.Enabled(FlagBylaw9Verified) {
		return TaxPoint{}, ErrDisabled
	}
	if amountCents <= 0 || b.ID == "" || appliedOn.IsZero() || b.TaxCents < 0 {
		return TaxPoint{}, fmt.Errorf("retainer application: %w", ErrInvalid)
	}
	if b.IssuedOn.IsZero() {
		return TaxPoint{}, fmt.Errorf("retainer application to bill %s: %w", b.ID, ErrNotDelivered)
	}
	// rule: BIL-T-051 (verify)
	// Tax arises only when the retainer is applied, and it references the issued bill;
	// applying it does not change the bill's tax.
	on := b.IssuedOn
	if appliedOn.Before(on) {
		on = appliedOn
	}
	return TaxPoint{BillID: b.ID, On: on, TaxCents: b.TaxCents}, nil
}
