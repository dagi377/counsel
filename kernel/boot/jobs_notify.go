package boot

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/webhook"
)

// KindWebhookEndpointExpiry is the daily job that warns before outbound endpoints expire.
const KindWebhookEndpointExpiry = "webhook.endpoint_expiry"

// webhookEndpointExpiryEvery is how often expiring endpoints are checked.
const webhookEndpointExpiryEvery = 24 * time.Hour

// subjectKindWebhookEndpoint is the outbox subject kind of an endpoint expiry event.
const subjectKindWebhookEndpoint = "webhook_endpoint"

// countingSender is the notification sender used when none is configured. It counts
// deliveries and drops them; it never logs the recipient, the template data or the text.
type countingSender struct{ n atomic.Int64 }

func (s *countingSender) Deliver(ctx context.Context, n notify.Rendered) error {
	c := s.n.Add(1)
	slog.DebugContext(ctx, "notification dropped: no sender configured", "channel", string(n.Channel), "dropped", c)
	return nil
}

// Count returns how many notifications were dropped.
func (s *countingSender) Count() int64 { return s.n.Load() }

type webhookEndpointExpiryArgs struct{}

func (webhookEndpointExpiryArgs) Kind() string { return KindWebhookEndpointExpiry }

// webhookEndpointExpiryWorker emits webhook_endpoint.expiring for endpoints due a notice.
type webhookEndpointExpiryWorker struct {
	river.WorkerDefaults[webhookEndpointExpiryArgs]
	k *Kernel
}

func (w webhookEndpointExpiryWorker) Work(ctx context.Context, _ *river.Job[webhookEndpointExpiryArgs]) error {
	emit := func(ctx context.Context, typ string, n webhook.ExpiringNotice) error {
		return w.k.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			s := outbox.Subject{Kind: subjectKindWebhookEndpoint, ID: n.EndpointID}
			return w.k.Emitter.Emit(ctx, tx, s, typ, n)
		})
	}
	// rule: ARC-I-049
	// ClaimNotice in NotifyExpiring makes a second run for the same notice emit nothing.
	if _, err := webhook.NotifyExpiring(ctx, w.k.WebhookExpiry, emit, w.k.Clock.Now()); err != nil {
		return fmt.Errorf("%s: %w", KindWebhookEndpointExpiry, err)
	}
	return nil
}

// addNotifyJobs adds the notification delivery worker as an outbox consumer when the kernel
// has a notify service, and the daily endpoint expiry job when it has an expiry store. It runs
// after every app's Register, so an app's NotifySender is in place; with none, deliveries go
// to a sender that only counts them.
func addNotifyJobs(k *Kernel) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("add notify jobs: no job set: %w", ErrConfig)
	}
	if k.Notify != nil {
		if k.NotifySender == nil {
			k.NotifySender = &countingSender{}
		}
		w := &notify.Worker{Service: k.Notify, Senders: map[notify.Channel]notify.Sender{
			notify.InApp: k.NotifySender, notify.Email: k.NotifySender,
			notify.SMS: k.NotifySender, notify.Push: k.NotifySender,
		}}
		if err := k.AddConsumer(w.Consumer()); err != nil {
			return fmt.Errorf("add notify jobs: %w", err)
		}
	}
	if k.WebhookExpiry != nil {
		if k.Emitter == nil || k.DB == nil || k.Clock == nil {
			return fmt.Errorf("add notify jobs: endpoint expiry needs an emitter, a DB and a clock: %w", ErrConfig)
		}
		// rule: ARC-I-049
		if err := AddWorker[webhookEndpointExpiryArgs](k.Jobs, webhookEndpointExpiryWorker{k: k}); err != nil {
			return fmt.Errorf("add notify jobs: %w", err)
		}
		k.Jobs.AddPeriodic(periodicEvery(webhookEndpointExpiryEvery, webhookEndpointExpiryArgs{}), KindWebhookEndpointExpiry)
	}
	return nil
}
