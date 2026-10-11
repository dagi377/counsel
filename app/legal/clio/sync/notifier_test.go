package sync

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/notify/notifyfake"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

const testTemplate = "sync.subscription_renewal_failed"

// fakeTx runs fn with a nil transaction; the fakes ignore it.
type fakeTx struct{ calls int }

func (f *fakeTx) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	f.calls++
	return fn(ctx, nil)
}

func newTestNotifier(t *testing.T, recipients []string) (*OutboxNotifier, *outboxfake.Fake, *notifyfake.Fake) {
	t.Helper()
	out, nf := outboxfake.New(), notifyfake.New()
	if err := nf.RegisterTemplate(notify.Template{ID: testTemplate, Channels: []notify.Channel{notify.InApp, notify.Email},
		Subject: "{{subscription_id}}", Body: "{{source}} {{expires_at}}"}); err != nil {
		t.Fatal(err)
	}
	n, err := NewOutboxNotifier(&fakeTx{}, out, nf, testTemplate, recipients)
	if err != nil {
		t.Fatal(err)
	}
	return n, out, nf
}

// rule: MIG-042
// rule: ARC-I-049
func TestMIG_042_OutboxNotifierRenewal(t *testing.T) {
	expires := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		now        time.Time
		fail       bool
		wantEvents int
		wantAlerts int // notifications per recipient per channel
	}{
		{"8 days left: not due", time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC), false, 0, 0},
		{"7 days left: renewed", time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC), false, 0, 0},
		{"6 days left, source fails: event, no alert", time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC), true, 1, 0},
		{"exactly 72 h left, source fails: no alert yet", time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), true, 1, 0},
		{"under 72 h left, source fails: alert", time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC), true, 1, 1},
	}
	recipients := []string{"admin-1", "oncall-1"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memStore{rows: map[string]*Subscription{
				"s1": {ID: "s1", Source: "clio", RemoteID: "r1", Events: []string{"matter"}, ExpiresAt: expires},
			}}
			n, out, nf := newTestNotifier(t, recipients)
			job := &RenewJob{Store: store, Source: &fakeSource{fail: tc.fail}, Notifier: n, Clock: clockfake.New(tc.now, time.UTC)}
			if _, err := job.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			msgs := out.Messages()
			if len(msgs) != tc.wantEvents {
				t.Fatalf("events = %d, want %d", len(msgs), tc.wantEvents)
			}
			for _, m := range msgs {
				if m.Type != EventRenewalFailed || m.Subject.Kind != SubjectKindSubscription || m.Subject.ID != "s1" {
					t.Errorf("event = %s %v, want %s on %s/s1", m.Type, m.Subject, EventRenewalFailed, SubjectKindSubscription)
				}
				var f RenewalFailure
				if err := json.Unmarshal(m.Payload, &f); err != nil || f.SubscriptionID != "s1" || !f.ExpiresAt.Equal(expires) {
					t.Errorf("payload = %s (%v)", m.Payload, err)
				}
			}
			sent := nf.Sent()
			if want := tc.wantAlerts * len(recipients) * 2; len(sent) != want {
				t.Fatalf("notifications = %d, want %d", len(sent), want)
			}
			for _, s := range sent {
				if s.Template != testTemplate || s.Data["subscription_id"] != "s1" || s.Data["source"] != "clio" ||
					s.Data["expires_at"] != "2026-11-29T19:00:00-05:00" {
					t.Errorf("notification = %+v", s)
				}
			}
		})
	}
}

func TestOutboxNotifierErrors(t *testing.T) {
	f := RenewalFailure{SubscriptionID: "s1", Source: "clio", ExpiresAt: time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)}
	tests := []struct {
		name    string
		run     func(n *OutboxNotifier) error
		wantErr error
	}{
		{"emit with empty subscription id is refused", func(n *OutboxNotifier) error {
			return n.Emit(context.Background(), EventRenewalFailed, RenewalFailure{})
		}, outbox.ErrInvalidEvent},
		{"alert with unregistered template is refused", func(n *OutboxNotifier) error {
			n.template = "sync.unknown"
			return n.Alert(context.Background(), f)
		}, notify.ErrUnknownTemplate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, _, _ := newTestNotifier(t, []string{"admin-1"})
			err := tc.run(n)
			if err == nil {
				t.Fatal("want error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
	if _, err := NewOutboxNotifier(&fakeTx{}, outboxfake.New(), notifyfake.New(), testTemplate, nil); !errors.Is(err, ErrNoRecipients) {
		t.Errorf("no recipients: err = %v, want %v", err, ErrNoRecipients)
	}
}
