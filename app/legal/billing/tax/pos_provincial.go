package tax

import (
	"context"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/flags"
)

// provincialTax is the provincial sales tax each non-participating province levies on legal
// services: QC QST, BC PST, MB RST (stored as PST, labelled "RST") and SK PST.
//
// rule: BIL-T-020
var provincialTax = map[string]Component{"QC": QST, "BC": PST, "MB": PST, "SK": PST}

// ResolveGSTHST resolves the decision table's "GST/HST" at a place of supply under the CRA
// place-of-supply rules: HST if the place is a participating province, else GST. A ZERO
// place carries neither.
func ResolveGSTHST(place string) Component {
	// rule: BIL-T-010
	switch {
	case place == Zero:
		return None
	case participating[place]:
		return HST
	default:
		return GST
	}
}

// componentsIn returns the components at a place of supply: HST in a participating province;
// otherwise GST, plus the province's own sales tax only when the firm is registered for it
// there. An unregistered firm charges GST only and the bill carries the self-assessment
// review flag (billing).
//
// (verify) Whether an Ontario firm must register in BC, MB, SK or QC is unconfirmed, so a
// provincial line is refused with ErrUnverified while tax.verify_rules is off.
func componentsIn(ctx context.Context, prov string, registered []string) ([]Component, error) {
	gstHST := ResolveGSTHST(prov)
	c, ok := provincialTax[prov]
	// rule: BIL-T-020
	if gstHST == HST || !ok || !slices.Contains(registered, prov) {
		return []Component{gstHST}, nil
	}
	// rule: BIL-T-020 (verify)
	if !flags.Enabled(ctx, FlagVerifyRules) {
		return nil, fmt.Errorf("decide %s %s: %w", prov, c, ErrUnverified)
	}
	return []Component{GST, c}, nil
}
