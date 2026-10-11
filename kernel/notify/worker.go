package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrNoSender is returned by the worker for a channel no Sender was configured for. The
// message stays pending, so it is delivered once a sender is wired.
var ErrNoSender = errors.New("notify: no sender for channel")

// ConsumerName is the outbox consumer name of the delivery worker (platform.inbox).
const ConsumerName = "notify.delivery"

// Rendered is one notification ready for a channel: the template with its placeholders
// filled. Senders resolve the user's address themselves; Rendered carries only the user id.
type Rendered struct {
	MessageID string // outbox message id; senders use it as their idempotency key
	To        string // user id
	Template  string
	Channel   Channel
	Subject   string
	Body      string
}

// Sender delivers rendered notifications on one channel (an email relay, an SMS gateway,
// the in-app inbox). An error leaves the outbox message pending, so it is retried.
type Sender interface {
	Deliver(ctx context.Context, n Rendered) error
}

// Worker delivers EventRequested outbox messages. It runs as an outbox consumer, so every
// delivery goes through the outbox: a failed send marks the message failed and the relay
// retries it, and the inbox claim makes a repeat delivery a no-op.
type Worker struct {
	Service *Service
	Senders map[Channel]Sender
}

// Consumer returns the worker as an outbox consumer for outbox.Deliverer.
func (w *Worker) Consumer() outbox.Consumer {
	return outbox.Consumer{Name: ConsumerName, Handle: w.Handle}
}

// Handle implements outbox.Handler. Messages of other types are ignored. The user's
// preference is read again at delivery time, so a channel the user turned off after the
// notification was queued is never sent; a locked preference always is.
func (w *Worker) Handle(ctx context.Context, tx pgx.Tx, m outbox.Message) error {
	if m.Type != EventRequested {
		return nil
	}
	var r Requested
	if err := json.Unmarshal(m.Payload, &r); err != nil {
		return fmt.Errorf("deliver %s: decode: %w", m.ID, err)
	}
	t, ok := w.Service.Template(r.Template)
	if !ok {
		return fmt.Errorf("deliver %s: %q: %w", m.ID, r.Template, ErrUnknownTemplate)
	}
	if !validChannel(r.Channel) {
		return fmt.Errorf("deliver %s: %q: %w", m.ID, r.Channel, ErrUnknownChannel)
	}
	prefs, err := w.Service.store.Load(ctx, tx, r.To, r.Template)
	if err != nil {
		return fmt.Errorf("deliver %s: load preferences: %w", m.ID, err)
	}
	for _, p := range prefs {
		if p.Channel == r.Channel && !p.Enabled && !p.Locked {
			slog.InfoContext(ctx, "notification skipped: channel off", "message_id", m.ID, "template", r.Template, "channel", string(r.Channel))
			return nil
		}
	}
	s, ok := w.Senders[r.Channel]
	if !ok || s == nil {
		return fmt.Errorf("deliver %s on %s: %w", m.ID, r.Channel, ErrNoSender)
	}
	n := Rendered{
		MessageID: m.ID,
		To:        r.To,
		Template:  r.Template,
		Channel:   r.Channel,
		Subject:   render(t.Subject, r.Data),
		Body:      render(t.Body, r.Data),
	}
	if err := s.Deliver(ctx, n); err != nil {
		return fmt.Errorf("deliver %s on %s: %w", m.ID, r.Channel, err)
	}
	return nil
}

// render fills each placeholder with its data value; a placeholder with no value renders empty.
func render(text string, data map[string]string) string {
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		name := strings.TrimSpace(placeholder.FindStringSubmatch(match)[1])
		return data[name]
	})
}
