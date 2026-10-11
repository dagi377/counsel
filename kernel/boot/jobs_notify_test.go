package boot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/webhook"
)

// noPrefs is a preference store with no stored rows: every channel is on.
type noPrefs struct{}

func (noPrefs) Load(context.Context, pgx.Tx, string, string) ([]notify.Preference, error) {
	return nil, nil
}
func (noPrefs) Save(context.Context, notify.Preference) error { return nil }

type expiryKey struct {
	id   string
	exp  time.Time
	lead time.Duration
}

// memExpiry is an in-memory webhook.ExpiryStore.
type memExpiry struct {
	eps     []webhook.EndpointExpiry
	notices map[expiryKey]bool
}

func (m *memExpiry) Expiring(_ context.Context, before time.Time) ([]webhook.EndpointExpiry, error) {
	var out []webhook.EndpointExpiry
	for _, e := range m.eps {
		if e.ExpiresAt != nil && !e.ExpiresAt.After(before) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memExpiry) ClaimNotice(_ context.Context, id string, exp time.Time, lead time.Duration) (bool, error) {
	k := expiryKey{id, exp.UTC(), lead}
	if m.notices[k] {
		return false, nil
	}
	m.notices[k] = true
	return true, nil
}

func (m *memExpiry) SetExpiry(context.Context, string, *time.Time) error { return nil }

func TestAddNotifyJobsKinds(t *testing.T) {
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		kernel        func() *Kernel
		wantKinds     []string
		wantConsumers []string
		wantErr       error
	}{
		{name: "nothing configured -> nothing added", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs()}
		}, wantKinds: []string{}},
		{name: "notify -> delivery consumer on the relay", kernel: func() *Kernel {
			ob := outboxfake.New()
			return &Kernel{Jobs: NewJobs(), Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob}, Notify: notify.New(noPrefs{}, ob)}
		}, wantKinds: []string{}, wantConsumers: []string{notify.ConsumerName}},
		{name: "notify without outbox -> config error", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs(), Notify: notify.New(noPrefs{}, outboxfake.New())}
		}, wantErr: ErrConfig},
		{name: "expiry store -> daily kind", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs(), DB: dbfake.New(), Clock: clockfake.New(now, time.UTC),
				Emitter: outboxfake.New(), WebhookExpiry: &memExpiry{}}
		}, wantKinds: []string{KindWebhookEndpointExpiry}},
		{name: "expiry store without emitter -> config error", kernel: func() *Kernel {
			return &Kernel{Jobs: NewJobs(), DB: dbfake.New(), Clock: clockfake.New(now, time.UTC), WebhookExpiry: &memExpiry{}}
		}, wantErr: ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := tt.kernel()
			err := addNotifyJobs(k)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("addNotifyJobs() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := k.Jobs.Kinds(); !reflect.DeepEqual(got, tt.wantKinds) {
				t.Fatalf("Kinds() = %v, want %v", got, tt.wantKinds)
			}
			if err := k.Jobs.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			var got []string
			if k.Outbox != nil {
				for _, c := range k.Outbox.Consumers {
					got = append(got, c.Name)
				}
			}
			if !reflect.DeepEqual(got, tt.wantConsumers) {
				t.Fatalf("consumers = %v, want %v", got, tt.wantConsumers)
			}
		})
	}
}

// TestARC_I_049_EndpointExpiryJob: an endpoint expiring in 14 days emits
// webhook_endpoint.expiring once; a second run emits nothing.
func TestARC_I_049_EndpointExpiryJob(t *testing.T) {
	now := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	in := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	tests := []struct {
		name      string
		endpoints []webhook.EndpointExpiry
		runs      int
		want      int
	}{
		// rule: ARC-I-049
		{name: "expiring in 14 days -> one event", endpoints: []webhook.EndpointExpiry{{ID: "ep1", ExpiresAt: in(14 * 24 * time.Hour)}}, runs: 1, want: 1},
		{name: "second run emits nothing", endpoints: []webhook.EndpointExpiry{{ID: "ep1", ExpiresAt: in(14 * 24 * time.Hour)}}, runs: 2, want: 1},
		{name: "endpoint that never expires -> nothing", endpoints: []webhook.EndpointExpiry{{ID: "ep2"}}, runs: 1, want: 0},
		{name: "expiring in 30 days -> nothing yet", endpoints: []webhook.EndpointExpiry{{ID: "ep3", ExpiresAt: in(30 * 24 * time.Hour)}}, runs: 1, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ob := outboxfake.New()
			k := &Kernel{Jobs: NewJobs(), DB: dbfake.New(), Clock: clockfake.New(now, time.UTC), Emitter: ob,
				WebhookExpiry: &memExpiry{eps: tt.endpoints, notices: map[expiryKey]bool{}}}
			if err := addNotifyJobs(k); err != nil {
				t.Fatal(err)
			}
			w := webhookEndpointExpiryWorker{k: k}
			for i := 0; i < tt.runs; i++ {
				if err := w.Work(ctx, &river.Job[webhookEndpointExpiryArgs]{}); err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
			}
			msgs := ob.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			for _, m := range msgs {
				if m.Type != webhook.EventEndpointExpiring || m.Subject.Kind != subjectKindWebhookEndpoint {
					t.Fatalf("event = %s on %s, want %s on %s", m.Type, m.Subject.Kind, webhook.EventEndpointExpiring, subjectKindWebhookEndpoint)
				}
			}
		})
	}
}

// TestCountingSenderKeepsDataOutOfLogs: with no NotifySender, a notification is delivered to
// the count-only sender, and neither the recipient nor the template data or text reach slog.
func TestCountingSenderKeepsDataOutOfLogs(t *testing.T) {
	const (
		recipient = "user-7f3a-recipient"
		secret    = "Wexford-Holdings-private"
		bodyText  = "Statement ready for"
	)
	tests := []struct {
		name    string
		channel notify.Channel
	}{
		{name: "email", channel: notify.Email},
		{name: "in app", channel: notify.InApp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ctx := context.Background()
			ob := outboxfake.New()
			svc := notify.New(noPrefs{}, ob)
			if err := svc.RegisterTemplate(notify.Template{
				ID: "desk.statement_ready", Channels: []notify.Channel{notify.Email, notify.InApp},
				Subject: "Statement {{name}}", Body: bodyText + " {{name}}",
			}); err != nil {
				t.Fatal(err)
			}
			pool := dbfake.New()
			k := &Kernel{Jobs: NewJobs(), DB: pool, Notify: svc, Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob}}
			if err := addKernelJobs(k); err != nil {
				t.Fatal(err)
			}
			if err := addNotifyJobs(k); err != nil {
				t.Fatal(err)
			}
			sender, ok := k.NotifySender.(*countingSender)
			if !ok {
				t.Fatalf("NotifySender = %T, want the count-only sender", k.NotifySender)
			}
			if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return svc.Send(ctx, tx, recipient, "desk.statement_ready", map[string]string{"name": secret})
			}); err != nil {
				t.Fatal(err)
			}
			if err := (outboxRelayWorker{k: k}).Work(ctx, &river.Job[outboxRelayArgs]{}); err != nil {
				t.Fatal(err)
			}
			if sender.Count() == 0 {
				t.Fatalf("count-only sender delivered nothing")
			}
			out := buf.String()
			for _, leak := range []string{recipient, secret, bodyText} {
				if strings.Contains(out, leak) {
					t.Fatalf("slog output contains %q:\n%s", leak, out)
				}
			}
		})
	}
}
