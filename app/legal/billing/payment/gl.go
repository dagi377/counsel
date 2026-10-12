package payment

import (
	"errors"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/ledger"
)

// Operating receipt journal sources. Each posts once per source id (ledger idempotency).
const (
	// ReceiptGLSource is the entry a captured payment posts at receipt; the source id is the
	// billing.payment id.
	ReceiptGLSource = "payment_receipt"
	// ReceiptReversalGLSource is the mirror of the receipt entry when the payment is
	// reversed; the source id is the billing.payment id.
	ReceiptReversalGLSource = "payment_receipt_reversal"
)

// AcctUnapplied holds received operating money until an approved allocation applies it to a
// bill: the client credit (unapplied) liability of the operating chart.
const AcctUnapplied = books.AcctClientCredit

// ErrNoGL is returned by Capture and Reverse while PGService has no GL or Clock: money is
// never received or reversed without its journal.
var ErrNoGL = errors.New("payment: general ledger not wired")

// operatingEntry is a two-line operating-book entry: Dr debit / Cr credit for cents.
func operatingEntry(source, sourceID, paymentID, memo string, on ledger.Date, debit, credit string, cents int64) ledger.Entry {
	amt := ledger.Cents(cents)
	subject := ledger.Subject{Kind: SubjectKind, ID: paymentID}
	return ledger.Entry{Book: books.BookOperating, Date: on, SourceKind: source, SourceID: sourceID, Memo: memo,
		Lines: []ledger.Line{
			{Account: debit, Side: ledger.Debit, Amount: amt, Subject: subject},
			{Account: credit, Side: ledger.Credit, Amount: amt, Subject: subject},
		}}
}

// receiptEntry is Dr operating bank / Cr unapplied for the whole payment, at receipt.
//
// rule: BIL-L-042
func receiptEntry(paymentID string, cents int64, on ledger.Date) ledger.Entry {
	return operatingEntry(ReceiptGLSource, paymentID, paymentID, "payment received", on,
		books.AcctOperatingBank, AcctUnapplied, cents)
}

// receiptReversalEntry mirrors receiptEntry: Dr unapplied / Cr operating bank.
//
// rule: BIL-I-019
func receiptReversalEntry(paymentID string, cents int64, on ledger.Date) ledger.Entry {
	return operatingEntry(ReceiptReversalGLSource, paymentID, paymentID, "payment reversed", on,
		AcctUnapplied, books.AcctOperatingBank, cents)
}

// applyEntry applies one bill allocation row: Dr unapplied / Cr AR fees. A negative row (a
// reversal row) posts the mirror, Dr AR fees / Cr unapplied. The source id is the
// billing.payment_allocation id.
//
// rule: BIL-L-042
func applyEntry(allocationID, paymentID string, cents int64, on ledger.Date) ledger.Entry {
	if cents < 0 {
		return operatingEntry(AllocationGLSource, allocationID, paymentID, "payment allocation reversed", on,
			books.AcctARFees, AcctUnapplied, -cents)
	}
	return operatingEntry(AllocationGLSource, allocationID, paymentID, "payment allocation", on,
		AcctUnapplied, books.AcctARFees, cents)
}
