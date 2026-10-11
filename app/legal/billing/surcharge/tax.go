package surcharge

import "fmt"

// RefundNext returns the surcharge and tax to refund when refunded more of appliedCents is
// refunded, after priorRefunded was refunded earlier and prior is what those earlier refunds
// returned. A partial refund is pro rata, as Refund computes it, capped at what is left. The
// refund that brings the total to appliedCents returns exactly the remaining surcharge and
// tax, so the refunds of one payment always sum to the original charge.
//
// Refunds are not gated by Flag: a surcharge already collected must stay refundable after
// the firm turns the feature off.
//
// rule: BIL-C-102
// rule: BIL-T-044 (verify)
func (c Calc) RefundNext(ch, prior Charge, priorRefunded, refunded, appliedCents int64) (Charge, error) {
	if priorRefunded < 0 || refunded < 0 || appliedCents <= 0 || priorRefunded+refunded > appliedCents {
		return Charge{}, fmt.Errorf("surcharge refund %d after %d of %d: %w", refunded, priorRefunded, appliedCents, ErrInvalid)
	}
	left, err := remaining(ch, prior)
	if err != nil {
		return Charge{}, err
	}
	if priorRefunded+refunded == appliedCents {
		return left, nil
	}
	r, err := c.Refund(ch, refunded, appliedCents)
	if err != nil {
		return Charge{}, err
	}
	out := Charge{RatePPM: ch.RatePPM, Cents: min(r.Cents, left.Cents)}
	out.TotalCents = out.Cents
	for i, t := range r.Tax {
		v := min(t.Cents, left.Tax[i].Cents)
		out.Tax = append(out.Tax, TaxLine{Code: t.Code, Cents: v})
		out.TotalCents += v
	}
	return out, nil
}

// remaining is ch less what prior refunds returned, line by line by tax code. It refuses a
// prior that exceeds the charge or names a tax code the charge does not carry.
func remaining(ch, prior Charge) (Charge, error) {
	if prior.Cents < 0 || prior.Cents > ch.Cents {
		return Charge{}, fmt.Errorf("surcharge refunded %d of %d: %w", prior.Cents, ch.Cents, ErrInvalid)
	}
	done := make(map[string]int64, len(prior.Tax))
	for _, t := range prior.Tax {
		done[t.Code] += t.Cents
	}
	left := Charge{RatePPM: ch.RatePPM, Cents: ch.Cents - prior.Cents}
	left.TotalCents = left.Cents
	for _, t := range ch.Tax {
		d := done[t.Code]
		delete(done, t.Code)
		if d < 0 || d > t.Cents {
			return Charge{}, fmt.Errorf("surcharge tax %s refunded %d of %d: %w", t.Code, d, t.Cents, ErrInvalid)
		}
		left.Tax = append(left.Tax, TaxLine{Code: t.Code, Cents: t.Cents - d})
		left.TotalCents += t.Cents - d
	}
	if len(done) > 0 {
		return Charge{}, fmt.Errorf("surcharge refund names a tax code the charge lacks: %w", ErrInvalid)
	}
	return left, nil
}
