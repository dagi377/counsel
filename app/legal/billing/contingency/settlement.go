package contingency

import (
	"net/http"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// Book is the ledger book a settlement receipt is recorded in.
type Book string

// Books a settlement receipt may name. Only BookTrust is accepted on a CFA matter.
const (
	BookTrust     Book = "trust"
	BookOperating Book = "operating"
)

// LineKind names one line of a settlement statement.
type LineKind string

// Settlement statement lines, in the order they are paid from the recovery.
const (
	LineDisbursements   LineKind = "reimbursed_disbursements"
	LineDisbursementTax LineKind = "reimbursed_disbursement_taxes"
	LineFee             LineKind = "fee"
	LineFeeTax          LineKind = "fee_taxes"
	LineBalanceToClient LineKind = "balance_to_client"
)

const (
	settlementLineCount  = 5
	codeMustDepositTrust = "MUST_DEPOSIT_TO_TRUST"
	codeDirectionMissing = "CLIENT_DIRECTION_REQUIRED"
	codeBadSettlement    = "CFA_BAD_SETTLEMENT"
)

// Settlement is the money side of a CFA matter's award or settlement. It carries no client data.
type Settlement struct {
	MatterID string
	// ReceiptBook is the book the settlement funds were deposited to (BIL-L-022).
	ReceiptBook   Book
	RecoveryCents money.Cents
	// DisbursementsCents and DisbursementTaxCents are what the firm paid and is reimbursed (BIL-L-023).
	DisbursementsCents   money.Cents
	DisbursementTaxCents money.Cents
	FeeCents             money.Cents
	FeeTaxCents          money.Cents
	// ClientDirectionDocID links the client's direction to pay the lawyer in trust, and
	// DirectionRecordedAt is when it was recorded (BIL-L-022).
	ClientDirectionDocID string
	DirectionRecordedAt  time.Time
	// LegalAidCertificate marks a matter whose first charge is subject to the Legal Aid
	// Services Act, 2020 s.13 (BIL-L-023).
	LegalAidCertificate bool
}

// StatementLine is one line of a settlement statement. ClaimedCents is what was owed and
// PaidCents what the recovery covers once earlier lines are paid.
type StatementLine struct {
	Kind         LineKind
	ClaimedCents money.Cents
	PaidCents    money.Cents
}

// Statement is a settlement statement. Lines are in payment order and the last line is the
// balance to the client. ReviewLegalAid asks a lawyer to review the first charge.
type Statement struct {
	MatterID       string
	Lines          []StatementLine
	ReviewLegalAid bool
}

// CheckReceipt refuses a CFA settlement receipt recorded anywhere but trust.
func (s Agreements) CheckReceipt(st Settlement) error {
	if err := s.on(); err != nil {
		return err
	}
	// rule: BIL-L-022
	if st.ReceiptBook != BookTrust {
		return cfaProblem(http.StatusUnprocessableEntity, codeMustDepositTrust, "BIL-L-022",
			"a settlement on a contingency matter must be deposited to trust")
	}
	return nil
}

// SettlementStatement builds the statement for st: the firm's disbursements and their taxes
// first, then the fee and its taxes, then the balance to the client. When the recovery does
// not cover every claim, earlier lines are paid in full before later ones get anything.
func (s Agreements) SettlementStatement(st Settlement) (Statement, error) {
	if err := s.CheckReceipt(st); err != nil {
		return Statement{}, err
	}
	claims := []StatementLine{
		// rule: BIL-L-023
		{Kind: LineDisbursements, ClaimedCents: st.DisbursementsCents},
		{Kind: LineDisbursementTax, ClaimedCents: st.DisbursementTaxCents},
		{Kind: LineFee, ClaimedCents: st.FeeCents},
		{Kind: LineFeeTax, ClaimedCents: st.FeeTaxCents},
	}
	if st.RecoveryCents < 0 {
		return Statement{}, cfaProblem(http.StatusUnprocessableEntity, codeBadSettlement, "BIL-L-023",
			"settlement amounts must be non-negative")
	}
	left := st.RecoveryCents
	out := Statement{MatterID: st.MatterID, Lines: make([]StatementLine, 0, settlementLineCount)}
	for _, c := range claims {
		if c.ClaimedCents < 0 {
			return Statement{}, cfaProblem(http.StatusUnprocessableEntity, codeBadSettlement, "BIL-L-023",
				"settlement amounts must be non-negative")
		}
		c.PaidCents = min(c.ClaimedCents, left)
		left -= c.PaidCents
		out.Lines = append(out.Lines, c)
	}
	out.Lines = append(out.Lines, StatementLine{Kind: LineBalanceToClient, ClaimedCents: left, PaidCents: left})
	// rule: BIL-L-023
	out.ReviewLegalAid = st.LegalAidCertificate
	return out, nil
}

// CheckPaymentFromRecovery refuses any payment out of the recovery at at unless the client's
// direction to pay the lawyer in trust was recorded at or before at.
func (s Agreements) CheckPaymentFromRecovery(st Settlement, at time.Time) error {
	if err := s.CheckReceipt(st); err != nil {
		return err
	}
	// rule: BIL-L-022
	if st.ClientDirectionDocID == "" || st.DirectionRecordedAt.IsZero() || st.DirectionRecordedAt.After(at) {
		return cfaProblem(http.StatusConflict, codeDirectionMissing, "BIL-L-022",
			"the client's direction must be recorded before any payment from the recovery")
	}
	return nil
}
