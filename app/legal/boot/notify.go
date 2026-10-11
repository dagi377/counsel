package boot

import (
	"context"
	"fmt"
	"log/slog"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/notify"
)

// Notification template ids the legal app registers.
const (
	// NotifySubscriptionRenewalFailed alerts the firm admin that a source subscription failed
	// to renew and is about to lapse (cliosync RenewalNotifier.Alert).
	NotifySubscriptionRenewalFailed = "sync.subscription_renewal_failed"
)

// notifyTemplates returns every notification template the legal app registers. Templates
// carry ids and times only, never client content.
func notifyTemplates() []notify.Template {
	return []notify.Template{
		{
			ID:       NotifySubscriptionRenewalFailed,
			Channels: []notify.Channel{notify.InApp, notify.Email},
			Subject:  "Source sync subscription {{subscription_id}} failed to renew",
			Body: "The {{source}} sync subscription {{subscription_id}} could not be renewed and " +
				"expires at {{expires_at}}. Sync stops when it lapses; check the connection.",
		},
	}
}

// registerNotify registers the app's notification templates with the kernel notify service
// and, when Options.Sender is set, makes it the kernel's notification sender. With no Sender
// the kernel keeps its count-only sender. It runs at Register, before the kernel adds the
// notification delivery worker.
func (o Options) registerNotify(ctx context.Context, k *kboot.Kernel) error {
	if o.Sender != nil {
		k.NotifySender = o.Sender
	}
	if k.Notify == nil {
		return nil
	}
	return registerTemplates(ctx, k.Notify, notifyTemplates())
}

// registerTemplates adds every template in ts to n. A duplicate id fails naming the id. Only
// ids are logged; subject and body text never are.
func registerTemplates(ctx context.Context, n notify.Notifier, ts []notify.Template) error {
	ids := make([]string, 0, len(ts))
	for _, t := range ts {
		if err := n.RegisterTemplate(t); err != nil {
			return fmt.Errorf("notify template %q: %w", t.ID, err)
		}
		ids = append(ids, t.ID)
	}
	slog.InfoContext(ctx, "notification templates registered", "count", len(ids), "ids", ids)
	return nil
}
