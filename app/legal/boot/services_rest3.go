package boot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/notify"
)

// Notification templates the deadline reminder jobs send through (BOOT-REST-3). They carry
// ids, dates and counts only, never deadline content.
var deadlineTemplates = []notify.Template{
	{ID: deadline.NoticeReminder, Channels: []notify.Channel{notify.InApp, notify.Email},
		Subject: "Deadline {{deadline_id}} is due {{due}}",
		Body:    "Deadline {{deadline_id}} is due on {{due}}, {{days_left}} days from {{day}}."},
	{ID: deadline.NoticeDigest, Channels: []notify.Channel{notify.InApp, notify.Email},
		Subject: "{{count}} deadlines due soon",
		Body:    "{{count}} deadlines are due in the next days as of {{day}}."},
}

// NotifyNotifier delivers deadline notices through the kernel notification service to one
// recipient, in its own transaction. It sends ids and dates only.
type NotifyNotifier struct {
	Service notify.Notifier
	DB      interface {
		InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
	}
	// To is the recipient user id.
	To string
}

// Notify implements deadline.Notifier.
func (n NotifyNotifier) Notify(ctx context.Context, x deadline.Notice) error {
	data := map[string]string{"day": dateText(x.Day), "count": strconv.Itoa(len(x.Items))}
	if x.Kind == deadline.NoticeReminder && len(x.Items) == 1 {
		it := x.Items[0]
		data["deadline_id"] = string(it.DeadlineID)
		data["due"] = dateText(it.Due)
		data["days_left"] = strconv.Itoa(it.DaysLeft)
	}
	if err := n.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return n.Service.Send(ctx, tx, n.To, x.Kind, data)
	}); err != nil {
		return fmt.Errorf("deadline notice %s: %w", x.Kind, err)
	}
	return nil
}

// withDeadlineDefaults fills the reminder ports from the kernel: the Postgres deadline book
// lists, the durable sent log remembers, and with a Recipient the kernel notification service
// is the primary notifier. Ports already set are kept.
func withDeadlineDefaults(k *kboot.Kernel, rest *RestServices, p DeadlinePorts) (DeadlinePorts, error) {
	if p.Deadlines == nil && rest != nil && rest.Book != nil {
		p.Deadlines = rest.Book
	}
	if p.Sent == nil && k.DB != nil {
		p.Sent = deadline.PGSentLog{DB: k.DB}
	}
	if p.Primary == nil && p.Recipient != "" && k.Notify != nil && k.DB != nil {
		if err := registerTemplates(context.Background(), k.Notify, deadlineTemplates); err != nil {
			return p, fmt.Errorf("deadline jobs: %w", err)
		}
		p.Primary = NotifyNotifier{Service: k.Notify, DB: k.DB, To: p.Recipient}
	}
	return p, nil
}

// dateText is d as YYYY-MM-DD.
func dateText(d deadline.Date) string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}
