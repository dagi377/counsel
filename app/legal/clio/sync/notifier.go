package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
)

// SubjectKindSubscription is the outbox subject kind of a platform.source_subscription row.
const SubjectKindSubscription = "platform.source_subscription"

// ErrNoRecipients is returned by NewOutboxNotifier when no admin or on-call user is given.
var ErrNoRecipients = errors.New("clio/sync: renewal alert has no recipients")

// TxRunner runs fn in one database transaction (db.Pool implements it).
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// OutboxNotifier is the RenewalNotifier over the kernel outbox and notify service. Emit
// writes one outbox event; Alert sends Template to every recipient (the firm admin and the
// platform on-call), all in one transaction.
type OutboxNotifier struct {
	tx         TxRunner
	out        outbox.Emitter
	notify     notify.Notifier
	template   string
	recipients []string
	loc        *time.Location
}

var _ RenewalNotifier = (*OutboxNotifier)(nil)

// NewOutboxNotifier returns an OutboxNotifier. template is a registered notify template
// with the placeholders subscription_id, source and expires_at; recipients are user ids.
func NewOutboxNotifier(tx TxRunner, out outbox.Emitter, n notify.Notifier, template string, recipients []string) (*OutboxNotifier, error) {
	if len(recipients) == 0 {
		return nil, ErrNoRecipients
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return nil, fmt.Errorf("renewal notifier: load location: %w", err)
	}
	return &OutboxNotifier{tx: tx, out: out, notify: n, template: template,
		recipients: append([]string(nil), recipients...), loc: loc}, nil
}

// Emit writes an outbox event of type typ about the subscription. The payload holds ids
// and times only.
func (n *OutboxNotifier) Emit(ctx context.Context, typ string, f RenewalFailure) error {
	err := n.tx.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// rule: ARC-I-049
		return n.out.Emit(ctx, tx, outbox.Subject{Kind: SubjectKindSubscription, ID: f.SubscriptionID}, typ, f)
	})
	if err != nil {
		return fmt.Errorf("renewal notifier: emit %s: %w", typ, err)
	}
	return nil
}

// Alert sends the renewal-failed notification to every recipient in one transaction.
func (n *OutboxNotifier) Alert(ctx context.Context, f RenewalFailure) error {
	data := map[string]string{
		"subscription_id": f.SubscriptionID,
		"source":          f.Source,
		"expires_at":      f.ExpiresAt.In(n.loc).Format(time.RFC3339),
	}
	err := n.tx.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, to := range n.recipients {
			// rule: MIG-042
			if err := n.notify.Send(ctx, tx, to, n.template, data); err != nil {
				return fmt.Errorf("send to %s: %w", to, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("renewal notifier: alert %s: %w", f.SubscriptionID, err)
	}
	return nil
}
