package draft

import (
	"fmt"

	"github.com/dagi377/counsel/kernel/money"
)

// Markup returns the markup on a disbursement of kind: rhu(cost * ppm, 1e6) for an own cost.
// An agent disbursement takes no markup; asking for one returns ErrMarkupOnAgent. Fee and
// flat lines carry no markup and return 0.
//
// rule: BIL-C-041
// Markup applies to own (soft) costs only, never to agent disbursements.
func Markup(a money.Arithmetic, kind LineKind, costCents, ppm int64) (int64, error) {
	switch kind {
	case LineAgentDis:
		if ppm != 0 {
			return 0, fmt.Errorf("markup %d ppm: %w", ppm, ErrMarkupOnAgent)
		}
		return 0, nil
	case LineOwnCost:
		v, err := PercentOf(a, costCents, ppm)
		if err != nil {
			return 0, fmt.Errorf("markup: %w", err)
		}
		return v, nil
	}
	return 0, nil
}

// RebillTaxable says whether a disbursement line of kind is in the tax base on rebill.
//
// rule: BIL-T-033
// A markup on an own cost is taxable with the cost: the tax base is the billed line amount.
//
// rule: BIL-L-048 (verify)
// Disbursements paid as the client's agent carry no HST on rebill; own costs are taxable.
// The same outcome is required unconditionally by BIL-T-030, so it is not behind a flag.
func RebillTaxable(kind LineKind) bool {
	return kind == LineOwnCost
}
