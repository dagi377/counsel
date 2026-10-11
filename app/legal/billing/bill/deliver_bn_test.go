package bill

import (
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/tax"
)

type flagSet map[string]bool

func (f flagSet) Enabled(name string) bool { return f[name] }

const goodBN = "123456782RT0001"

// rule: BIL-L-047 (verify)
func TestBIL_L_047_DeliveryNeedsBN(t *testing.T) {
	on := tax.BN{Flags: flagSet{tax.FlagBNCheckVerified: true}}
	off := tax.BN{Flags: flagSet{}}
	b := numbered("INV-2026-0147", StatusApproved, 3)
	tests := []struct {
		name string
		firm FirmTax
		bn   tax.BNChecker
		err  error
	}{
		{"unregistered firm passes", FirmTax{}, on, nil},
		{"unregistered firm passes with flag off", FirmTax{}, off, nil},
		{"registrant with valid BN passes", FirmTax{Registrant: true, BN: goodBN}, on, nil},
		{"registrant without BN blocked", FirmTax{Registrant: true}, on, ErrMissingBN},
		{"registrant with bad check digit blocked", FirmTax{Registrant: true, BN: "123456789RT0001"}, on, ErrMissingBN},
		{"registrant with malformed BN blocked", FirmTax{Registrant: true, BN: "123456782"}, on, ErrMissingBN},
		{"flag off fails closed", FirmTax{Registrant: true, BN: goodBN}, off, tax.ErrUnverified},
		{"no checker fails closed", FirmTax{Registrant: true, BN: goodBN}, nil, tax.ErrUnverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckDeliverable(b, tc.firm, tc.bn)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil && tc.firm.BN != "" && strings.Contains(err.Error(), tc.firm.BN) {
				t.Fatalf("error leaks the business number: %v", err)
			}
			if errors.Is(err, ErrMissingBN) && !strings.Contains(err.Error(), CodeHSTNumberMissing) {
				t.Fatalf("error %v lacks code %s", err, CodeHSTNumberMissing)
			}
		})
	}
}

// rule: BIL-I-008
func TestBIL_I_008(t *testing.T) {
	n := func(number string, s Status) Bill {
		b := numbered(number, s, 3)
		b.ID = ID("b-" + number)
		return b
	}
	pending := pendingBill()
	unnumberedVoid := pendingBill()
	unnumberedVoid.Status = StatusVoid
	numberedDraft := n("INV-2026-0150", StatusDraft)
	unnumberedApproved := pendingBill()
	unnumberedApproved.Status = StatusApproved
	tests := []struct {
		name  string
		bills []Bill
		err   error
	}{
		{"consecutive with void kept", []Bill{n("INV-2026-0143", StatusPaid), n("INV-2026-0141", StatusDelivered), n("INV-2026-0142", StatusVoid)}, nil},
		{"drafts and unnumbered void have no number", []Bill{n("INV-2026-0001", StatusApproved), pending, unnumberedVoid}, nil},
		{"series counted apart", []Bill{n("INV-2026-0147", StatusApproved), n("INV-2027-0001", StatusApproved), n("INV-2026-0148", StatusApproved)}, nil},
		{"wide numbers continue", []Bill{n("INV-2026-9999", StatusApproved), n("INV-2026-10000", StatusApproved)}, nil},
		{"gap", []Bill{n("INV-2026-0141", StatusApproved), n("INV-2026-0143", StatusApproved)}, ErrNumbering},
		{"duplicate", []Bill{n("INV-2026-0141", StatusApproved), func() Bill { b := n("INV-2026-0141", StatusPaid); b.ID = "dup"; return b }()}, ErrNumbering},
		{"draft with number", []Bill{numberedDraft}, ErrNumbering},
		{"approved without number", []Bill{unnumberedApproved}, ErrNumbering},
		{"malformed number", []Bill{n("INV-2026-01", StatusApproved)}, ErrNumbering},
		{"empty set", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckNumbering(tc.bills); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}
