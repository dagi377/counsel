package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/webhook"
)

// KindWebhookDeliver is the job that sends queued outbound webhook deliveries. It is also the
// outbox consumer name that queues them.
const KindWebhookDeliver = "webhook.deliver"

const (
	// webhookDeliverEvery is how often queued deliveries are sent.
	webhookDeliverEvery = 5 * time.Second
	// webhookDeliverRounds caps how many deliveries one subject sends in one run.
	webhookDeliverRounds = 100
)

// WebhookQueue is the part of the T-036.2 dispatcher boot drives: queue a message for every
// subscribed endpoint, hand out the head of each subject's queue, and record the result.
// *webhook.Queue and webhookfake.Fake satisfy it.
type WebhookQueue interface {
	Enqueue(ctx context.Context, tx pgx.Tx, m webhook.Message) ([]webhook.DeliveryID, error)
	Ready(ctx context.Context, endpoint string) ([]webhook.Delivery, error)
	Ack(ctx context.Context, id webhook.DeliveryID) error
	Fail(ctx context.Context, id webhook.DeliveryID) error
}

var _ WebhookQueue = (*webhook.Queue)(nil)

// WebhookEndpoints lists the ids of the endpoints that have deliveries to send.
type WebhookEndpoints interface {
	Endpoints(ctx context.Context) ([]string, error)
}

// WebhookPoster sends one delivery, signed, to its endpoint. A nil error means the endpoint
// answered 2xx. The delivery's Message.ID is the webhook-id and is the same on every retry.
type WebhookPoster interface {
	Post(ctx context.Context, d webhook.Delivery) error
}

// webhookQueueConsumer queues each outbox message of a registered webhook event type for
// every subscribed endpoint, in the relay's transaction. The outbox message id becomes the
// stable webhook-id. Messages of types that are not webhook events are not queued.
func webhookQueueConsumer(q WebhookQueue) outbox.Consumer {
	return outbox.Consumer{Name: KindWebhookDeliver, Handle: func(ctx context.Context, tx pgx.Tx, m outbox.Message) error {
		_, err := q.Enqueue(ctx, tx, webhook.Message{
			ID: m.ID, Type: m.Type, Subject: m.Subject, Timestamp: m.CreatedAt, Payload: m.Payload,
		})
		if errors.Is(err, webhook.ErrUnknownType) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("queue webhook %s: %w", m.ID, err)
		}
		return nil
	}}
}

type webhookDeliverArgs struct{}

func (webhookDeliverArgs) Kind() string { return KindWebhookDeliver }

// webhookDeliverWorker sends the ready deliveries of every endpoint.
type webhookDeliverWorker struct {
	river.WorkerDefaults[webhookDeliverArgs]
	k *Kernel
}

func (w webhookDeliverWorker) Work(ctx context.Context, _ *river.Job[webhookDeliverArgs]) error {
	eps, err := w.k.WebhookEndpoints.Endpoints(ctx)
	if err != nil {
		return fmt.Errorf("%s: endpoints: %w", KindWebhookDeliver, err)
	}
	var errs []error
	for _, ep := range eps {
		if err := w.drain(ctx, ep); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%s: %w", KindWebhookDeliver, err)
	}
	return nil
}

// drain sends endpoint's deliveries until every subject is empty or blocked by a failure.
//
// rule: ARC-I-034
// A subject's next delivery is handed out only after the previous one is acked, so a failed
// delivery holds its subject back for the rest of this run while other subjects go on. The
// failed delivery is retried on a later run with the same webhook-id.
func (w webhookDeliverWorker) drain(ctx context.Context, endpoint string) error {
	q := w.k.Dispatcher
	blocked := map[webhook.Subject]bool{}
	for round := 0; round < webhookDeliverRounds; round++ {
		ready, err := q.Ready(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("endpoint %s: %w", endpoint, err)
		}
		sent := 0
		for _, d := range ready {
			if blocked[d.Message.Subject] {
				continue
			}
			if err := w.k.WebhookPoster.Post(ctx, d); err != nil {
				slog.WarnContext(ctx, "webhook delivery failed", "endpoint", endpoint, "delivery", string(d.ID),
					"type", d.Message.Type, "attempts", d.Attempts+1)
				blocked[d.Message.Subject] = true
				if ferr := q.Fail(ctx, d.ID); ferr != nil {
					return fmt.Errorf("endpoint %s: %w", endpoint, ferr)
				}
				continue
			}
			if err := q.Ack(ctx, d.ID); err != nil {
				return fmt.Errorf("endpoint %s: %w", endpoint, err)
			}
			sent++
		}
		if sent == 0 {
			return nil
		}
	}
	return nil
}

// addWebhookJobs adds outbound webhook delivery when the kernel has a Dispatcher: the outbox
// consumer that queues webhook events, and the periodic kind webhook.deliver that sends them.
// With no Dispatcher it adds nothing and is not an error.
func addWebhookJobs(k *Kernel) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("add webhook jobs: no job set: %w", ErrConfig)
	}
	if k.Dispatcher == nil {
		return nil
	}
	if k.WebhookEndpoints == nil || k.WebhookPoster == nil {
		return fmt.Errorf("add webhook jobs: dispatcher needs endpoints and a poster: %w", ErrConfig)
	}
	if err := k.AddConsumer(webhookQueueConsumer(k.Dispatcher)); err != nil {
		return fmt.Errorf("add webhook jobs: %w", err)
	}
	// rule: ARC-I-034
	if err := AddWorker[webhookDeliverArgs](k.Jobs, webhookDeliverWorker{k: k}); err != nil {
		return fmt.Errorf("add webhook jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(webhookDeliverEvery, webhookDeliverArgs{}), KindWebhookDeliver)
	return nil
}
