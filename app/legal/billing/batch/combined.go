package batch

import (
	"context"
	"fmt"
	"sort"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/money"
)

// TaxBase is the taxable base of one matter under one tax code.
type TaxBase struct {
	Code      string    // tax code, e.g. "HST-ON"
	RatePPM   money.PPM // rate of the code
	BaseCents int64
}

// TaxLine is one tax line of a matter on a combined bill.
type TaxLine struct {
	Code      string
	RatePPM   money.PPM
	BaseCents int64
	TaxCents  int64
}

// MatterPart is what one matter contributes to a combined client bill.
type MatterPart struct {
	MatterID      string
	FeesCents     int64
	DisbCents     int64
	DiscountCents int64
	TaxBases      []TaxBase
	TaxLines      []TaxLine // filled by Combine
	SubtotalCents int64     // fees + disbursements - discount, filled by Combine
	TaxCents      int64     // sum of TaxLines, filled by Combine
	TotalCents    int64     // subtotal + tax, filled by Combine
}

// Combined is one draft bill for one client over several matters.
type Combined struct {
	Key        string // idempotency key, see Key
	ClientID   string
	PeriodTo   clock.Date
	Matters    []MatterPart // sorted by MatterID
	TaxCents   int64
	TotalCents int64
}

// CombinedDrafts stores combined drafts. A key already stored returns the first draft id
// with created=false. Drafts go to the bill approval gate; nothing here approves.
type CombinedDrafts interface {
	CreateCombined(ctx context.Context, key string, c Combined) (draftID string, created bool, err error)
}

// Combine builds one combined draft for clientID over parts. Lines stay grouped by matter
// with per-matter subtotals and tax lines. Tax per code is computed once on the summed base
// and split back over the matters by base, so the matter tax lines add up to the bill's tax.
// Rerunning the same run stores no second draft.
func Combine(ctx context.Context, d CombinedDrafts, runID, clientID string, periodTo clock.Date, parts []MatterPart) (string, bool, Combined, error) {
	if runID == "" || clientID == "" || len(parts) == 0 {
		return "", false, Combined{}, fmt.Errorf("combined bill: %w", ErrInvalid)
	}
	c := Combined{
		Key:      Key(runID, Target{ClientID: clientID}, periodTo),
		ClientID: clientID,
		PeriodTo: periodTo,
		Matters:  make([]MatterPart, len(parts)),
	}
	seen := map[string]bool{}
	for i, p := range parts {
		if p.MatterID == "" || seen[p.MatterID] {
			return "", false, Combined{}, fmt.Errorf("combined bill matter %q: %w", p.MatterID, ErrInvalid)
		}
		seen[p.MatterID] = true
		p.TaxBases = append([]TaxBase(nil), p.TaxBases...)
		p.TaxLines = nil
		p.SubtotalCents = p.FeesCents + p.DisbCents - p.DiscountCents
		c.Matters[i] = p
	}
	sort.Slice(c.Matters, func(i, j int) bool { return c.Matters[i].MatterID < c.Matters[j].MatterID })
	if err := c.tax(); err != nil {
		return "", false, Combined{}, fmt.Errorf("combined bill tax: %w", err)
	}
	for i := range c.Matters {
		m := &c.Matters[i]
		m.TotalCents = m.SubtotalCents + m.TaxCents
		c.TotalCents += m.TotalCents
		c.TaxCents += m.TaxCents
	}
	// rule: BIL-C-110
	id, created, err := d.CreateCombined(ctx, c.Key, c)
	if err != nil {
		return "", false, Combined{}, fmt.Errorf("combined draft %s: %w", c.Key, err)
	}
	return id, created, c, nil
}

// tax computes each code's tax once on the summed base and allocates it to the matters by
// their base under that code.
//
// rule: BIL-C-111
func (c *Combined) tax() error {
	type code struct {
		rate  money.PPM
		total int64
	}
	codes := map[string]*code{}
	var order []string
	for _, m := range c.Matters {
		for _, b := range m.TaxBases {
			if b.BaseCents < 0 || b.RatePPM < 0 {
				return ErrInvalid
			}
			k, ok := codes[b.Code]
			if !ok {
				k = &code{rate: b.RatePPM}
				codes[b.Code] = k
				order = append(order, b.Code)
			} else if k.rate != b.RatePPM {
				return fmt.Errorf("code %s has two rates: %w", b.Code, ErrInvalid)
			}
			k.total += b.BaseCents
		}
	}
	sort.Strings(order)
	for _, name := range order {
		k := codes[name]
		tax, err := money.ApplyPPM(money.Cents(k.total), k.rate)
		if err != nil {
			return err
		}
		weights := make([]int64, len(c.Matters))
		for i, m := range c.Matters {
			for _, b := range m.TaxBases {
				if b.Code == name {
					weights[i] += b.BaseCents
				}
			}
		}
		shares := make([]money.Cents, len(c.Matters))
		if k.total > 0 {
			if shares, err = money.Allocate(tax, weights); err != nil {
				return err
			}
		}
		for i := range c.Matters {
			if weights[i] == 0 && shares[i] == 0 {
				continue
			}
			m := &c.Matters[i]
			m.TaxLines = append(m.TaxLines, TaxLine{Code: name, RatePPM: k.rate, BaseCents: weights[i], TaxCents: int64(shares[i])})
			m.TaxCents += int64(shares[i])
		}
	}
	return nil
}
