package draft

import (
	"fmt"
	"strings"
)

// CheckDelivery reports whether a draft is sufficient in form to deliver. It returns an error
// wrapping ErrDeliveryBlocked for the first line that fails.
//
// rule: BIL-L-002
// Fees may be a lump sum only with a services narrative. Every disbursement needs its own
// date, description and amount; an aggregated disbursement line needs that detail for each
// item it covers, otherwise delivery is blocked.
func CheckDelivery(lines []Line) error {
	for _, l := range lines {
		switch l.Kind {
		case LineFee, LineFlat:
			if blank(l.Description) {
				return fmt.Errorf("line %s: fee without services narrative: %w", l.ID, ErrDeliveryBlocked)
			}
		case LineOwnCost, LineAgentDis:
			if err := checkDisbursement(l); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDisbursement(l Line) error {
	if l.ItemCount <= 1 && len(l.Items) == 0 {
		if l.ItemDate == nil || l.ItemDate.IsZero() || blank(l.Description) || l.AmountCents == 0 {
			return fmt.Errorf("line %s: disbursement without date, description or amount: %w", l.ID, ErrDeliveryBlocked)
		}
		return nil
	}
	if l.ItemCount > 1 && len(l.Items) != l.ItemCount {
		return fmt.Errorf("line %s: %d of %d items itemized: %w", l.ID, len(l.Items), l.ItemCount, ErrDeliveryBlocked)
	}
	for i, it := range l.Items {
		if it.Date.IsZero() || blank(it.Description) || it.AmountCents == 0 {
			return fmt.Errorf("line %s item %d: without date, description or amount: %w", l.ID, i+1, ErrDeliveryBlocked)
		}
	}
	return nil
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }
