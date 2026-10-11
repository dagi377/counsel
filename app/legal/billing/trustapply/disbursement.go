package trustapply

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// GateTrustWithdrawal is the gate kind (spec G11) that authorizes a trust withdrawal.
const GateTrustWithdrawal = "trust.withdrawal"

// Disbursement errors.
var (
	// ErrNoWithdrawalGate is returned when a disbursement claims to be paid from trust
	// without an approved trust withdrawal gate (409).
	ErrNoWithdrawalGate = errors.New("trustapply: disbursement needs an approved trust withdrawal")
	// ErrNoClientDirection is returned when a disbursement paid from trust has no
	// client-direction record (409).
	ErrNoClientDirection = errors.New("trustapply: disbursement needs a client direction")
	// ErrUnverified is returned while the By-law 9 rules behind a trust check are unverified:
	// the check fails closed rather than passing silently.
	ErrUnverified = errors.New("trustapply: by-law 9 rules not verified")
)

// TrustDisbursement is an expense paid by a trust withdrawal.
type TrustDisbursement struct {
	ExpenseID          string
	MatterID           string
	AmountCents        int64
	GateID             string // the approved withdrawal gate
	GateKind           string // must be GateTrustWithdrawal
	GateApproved       bool
	GateAmountCents    int64  // the amount the gate approved
	ClientDirectionRef string // the client's written direction to pay this expense from trust
}

// BillDisbursement is one disbursement line on a bill.
type BillDisbursement struct {
	ExpenseID     string
	AmountCents   int64
	PaidFromTrust bool
}

// BillDisbursements is a bill's disbursements split into the amount due and the
// "Paid from trust" section.
type BillDisbursements struct {
	DueCents           int64
	PaidFromTrustCents int64
	PaidFromTrust      []BillDisbursement
}

// CheckDisbursement validates an expense paid from trust before it is listed on a bill.
// It fails closed: while FlagBylaw9Verified is off it returns ErrUnverified (also ErrDisabled).
func CheckDisbursement(ctx context.Context, d TrustDisbursement) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return fmt.Errorf("trust disbursement: %w: %w", ErrUnverified, ErrDisabled)
	}
	if d.AmountCents <= 0 || d.ExpenseID == "" || d.MatterID == "" {
		return fmt.Errorf("trust disbursement: %w", ErrInvalid)
	}
	// rule: BIL-C-042 (verify)
	// An expense paid from trust needs an approved trust withdrawal (G11) that covers it.
	if d.GateID == "" || d.GateKind != GateTrustWithdrawal || !d.GateApproved || d.GateAmountCents < d.AmountCents {
		return fmt.Errorf("trust disbursement %s: %w", d.ExpenseID, ErrNoWithdrawalGate)
	}
	// rule: BIL-C-042 (verify)
	// The client's direction to pay the expense from trust is on record.
	if d.ClientDirectionRef == "" {
		return fmt.Errorf("trust disbursement %s: %w", d.ExpenseID, ErrNoClientDirection)
	}
	return nil
}

// SplitDisbursements splits a bill's disbursement lines into amount due and paid from trust.
func SplitDisbursements(lines []BillDisbursement) (BillDisbursements, error) {
	var out BillDisbursements
	for _, l := range lines {
		if l.AmountCents < 0 {
			return BillDisbursements{}, fmt.Errorf("disbursement %s: %w", l.ExpenseID, ErrInvalid)
		}
		// rule: BIL-C-042 (verify)
		// A trust-paid expense is listed under "Paid from trust" and excluded from the
		// amount due and AR.
		if l.PaidFromTrust {
			out.PaidFromTrustCents += l.AmountCents
			out.PaidFromTrust = append(out.PaidFromTrust, l)
			continue
		}
		out.DueCents += l.AmountCents
	}
	return out, nil
}
