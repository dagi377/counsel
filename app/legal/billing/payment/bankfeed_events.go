package payment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// EventBankFeedLineImported is published once per operating bank-feed line imported.
// F-BIL-04 (payment matching) starts on it.
const EventBankFeedLineImported = "bank_feed.line_imported"

// SubjectBankFeedLine is the outbox subject kind of an imported bank-feed line.
const SubjectBankFeedLine = "billing.bank_feed_line"

// BankFeedLineImported is the body of EventBankFeedLineImported: ids, the amount in integer
// cents and the posting day. Never the payee text or memo.
type BankFeedLineImported struct {
	LineID      string `json:"line_id"`
	AccountID   string `json:"account_id"`
	AmountCents int64  `json:"amount_cents"`
	PostedOn    string `json:"posted_on"` // YYYY-MM-DD, America/Toronto
}

// PublishBankFeedLineImported publishes EventBankFeedLineImported in tx (nil: the emitter
// opens its own). Minimal publisher: main imports trust statements only and has no operating
// bank feed yet (Q-EVENTS-7).
func PublishBankFeedLineImported(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b BankFeedLineImported) error {
	if b.LineID == "" || b.AccountID == "" || b.PostedOn == "" {
		return fmt.Errorf("publish %s: %w", EventBankFeedLineImported, ErrInvalid)
	}
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, outbox.Subject{Kind: SubjectBankFeedLine, ID: b.LineID}, EventBankFeedLineImported, b); err != nil {
		return fmt.Errorf("publish %s: %w", EventBankFeedLineImported, err)
	}
	return nil
}
