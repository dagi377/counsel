package contingency

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

var settledAt = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

func settlement() Settlement {
	return Settlement{MatterID: "M-1", ReceiptBook: BookTrust, RecoveryCents: 1000000,
		DisbursementsCents: 50000, DisbursementTaxCents: 6500, FeeCents: 276000, FeeTaxCents: 35880,
		ClientDirectionDocID: "doc-dir", DirectionRecordedAt: settledAt}
}

// rule: BIL-L-022
func TestCheckReceipt(t *testing.T) {
	op := settlement()
	op.ReceiptBook = BookOperating
	op.RecoveryCents = 3000000
	tests := []struct {
		name   string
		st     Settlement
		code   string
		status int
	}{
		{"trust receipt accepted", settlement(), "", 0},
		{"BIL-L-022 cheque 3000000 to operating refused", op, codeMustDepositTrust, http.StatusUnprocessableEntity},
		{"no book refused", Settlement{RecoveryCents: 1}, codeMustDepositTrust, http.StatusUnprocessableEntity},
	}
	s := agreements(t, true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckReceipt(tc.st), tc.code, tc.status)
		})
	}
}

// rule: BIL-L-023
func TestSettlementStatement(t *testing.T) {
	short := settlement()
	short.RecoveryCents = 300000
	aid := settlement()
	aid.LegalAidCertificate = true
	op := settlement()
	op.ReceiptBook = BookOperating
	neg := settlement()
	neg.FeeCents = -1
	tests := []struct {
		name   string
		st     Settlement
		want   []StatementLine
		review bool
		code   string
		status int
	}{
		{"BIL-L-023 disbursements 50000 + HST 6500 listed first", settlement(), []StatementLine{
			{LineDisbursements, 50000, 50000}, {LineDisbursementTax, 6500, 6500},
			{LineFee, 276000, 276000}, {LineFeeTax, 35880, 35880}, {LineBalanceToClient, 631620, 631620},
		}, false, "", 0},
		{"short recovery pays disbursements first, fee gets the rest", short, []StatementLine{
			{LineDisbursements, 50000, 50000}, {LineDisbursementTax, 6500, 6500},
			{LineFee, 276000, 243500}, {LineFeeTax, 35880, 0}, {LineBalanceToClient, 0, 0},
		}, false, "", 0},
		{"legal aid certificate flagged for review", aid, nil, true, "", 0},
		{"BIL-L-022 operating receipt refused", op, nil, false, codeMustDepositTrust, http.StatusUnprocessableEntity},
		{"negative claim refused", neg, nil, false, codeBadSettlement, http.StatusUnprocessableEntity},
	}
	s := agreements(t, true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.SettlementStatement(tc.st)
			wantCode(t, err, tc.code, tc.status)
			if err != nil {
				return
			}
			if tc.want != nil && !reflect.DeepEqual(got.Lines, tc.want) {
				t.Fatalf("lines = %+v, want %+v", got.Lines, tc.want)
			}
			if got.ReviewLegalAid != tc.review {
				t.Fatalf("review = %v, want %v", got.ReviewLegalAid, tc.review)
			}
		})
	}
}

// rule: BIL-L-022
func TestCheckPaymentFromRecovery(t *testing.T) {
	none := settlement()
	none.ClientDirectionDocID = ""
	late := settlement()
	late.DirectionRecordedAt = settledAt.Add(time.Hour)
	unstamped := settlement()
	unstamped.DirectionRecordedAt = time.Time{}
	tests := []struct {
		name   string
		st     Settlement
		at     time.Time
		code   string
		status int
	}{
		{"direction recorded before payment", settlement(), settledAt.Add(time.Minute), "", 0},
		{"direction recorded at payment time", settlement(), settledAt, "", 0},
		{"no direction refused", none, settledAt, codeDirectionMissing, http.StatusConflict},
		{"direction recorded after payment refused", late, settledAt, codeDirectionMissing, http.StatusConflict},
		{"direction without a time refused", unstamped, settledAt, codeDirectionMissing, http.StatusConflict},
	}
	s := agreements(t, true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantCode(t, s.CheckPaymentFromRecovery(tc.st, tc.at), tc.code, tc.status)
		})
	}
}

func TestSettlementFlagOff(t *testing.T) {
	s := agreements(t, false)
	if _, err := s.SettlementStatement(settlement()); !errors.Is(err, ErrAgreementsOff) {
		t.Fatalf("err = %v, want ErrAgreementsOff", err)
	}
	if err := s.CheckPaymentFromRecovery(settlement(), settledAt); !errors.Is(err, ErrAgreementsOff) {
		t.Fatalf("err = %v, want ErrAgreementsOff", err)
	}
}
