package notify_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// txInbox is an Inbox whose claims commit only when the handler's transaction does, as
// platform.inbox does under Postgres.
type txInbox struct {
	mu      sync.Mutex
	done    map[string]bool
	pending []string
}

func (i *txInbox) Claim(_ context.Context, _ pgx.Tx, consumer, id string) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	k := consumer + "/" + id
	if i.done[k] {
		return false, nil
	}
	i.pending = append(i.pending, k)
	return true, nil
}

func (i *txInbox) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	err := fn(ctx, nil)
	i.mu.Lock()
	defer i.mu.Unlock()
	if err == nil {
		for _, k := range i.pending {
			i.done[k] = true
		}
	}
	i.pending = nil
	return err
}

// fakeSender records deliveries and fails the first `fail` calls.
type fakeSender struct {
	fail int
	got  []notify.Rendered
}

func (s *fakeSender) Deliver(_ context.Context, n notify.Rendered) error {
	if s.fail > 0 {
		s.fail--
		return errors.New("relay unavailable")
	}
	s.got = append(s.got, n)
	return nil
}

func newDeliverer(out *outboxfake.Fake, w *notify.Worker) *outbox.Deliverer {
	in := &txInbox{done: map[string]bool{}}
	return &outbox.Deliverer{Relay: out, Inbox: in, DB: in, Consumers: []outbox.Consumer{w.Consumer()}}
}

func TestWorkerDeliversThroughOutboxWithRetry(t *testing.T) {
	ctx := context.Background()
	data := map[string]string{"name": "Ada", "number": "B-7", "link": "https://x/b/7"}
	tests := []struct {
		name       string
		emailFails int
		rounds     int
		wantEmail  int
		wantFailed int // outbox attempts recorded on the email message
	}{
		{name: "sent first time", emailFails: 0, rounds: 1, wantEmail: 1},
		{name: "retried after failure", emailFails: 2, rounds: 3, wantEmail: 1, wantFailed: 2},
		{name: "still pending while failing", emailFails: 5, rounds: 2, wantEmail: 0, wantFailed: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, out := newService(t)
			email := &fakeSender{fail: tc.emailFails}
			inapp := &fakeSender{}
			w := &notify.Worker{Service: s, Senders: map[notify.Channel]notify.Sender{notify.Email: email, notify.InApp: inapp}}
			d := newDeliverer(out, w)
			if err := s.Send(ctx, nil, "u1", "billing.bill_ready", data); err != nil {
				t.Fatal(err)
			}
			for range tc.rounds {
				if _, err := d.Deliver(ctx, 10); err != nil {
					t.Fatal(err)
				}
			}
			if len(email.got) != tc.wantEmail {
				t.Fatalf("email sends = %d, want %d", len(email.got), tc.wantEmail)
			}
			if len(inapp.got) != 1 {
				t.Fatalf("in-app sends = %d, want 1 (no duplicate on retry rounds)", len(inapp.got))
			}
			if tc.wantEmail == 1 {
				g := email.got[0]
				if g.Subject != "Bill B-7 is ready" || g.Body != "Hello Ada, bill B-7 is ready: https://x/b/7" || g.To != "u1" || g.MessageID == "" {
					t.Fatalf("rendered = %+v", g)
				}
			}
			pending, _ := out.Pending(ctx, 10)
			if tc.wantEmail == 1 && len(pending) != 0 {
				t.Fatalf("pending after delivery = %d", len(pending))
			}
			if tc.wantEmail == 0 {
				if len(pending) != 1 || pending[0].Attempts != tc.wantFailed {
					t.Fatalf("pending = %+v, want one email message with %d attempts", pending, tc.wantFailed)
				}
			}
			for _, m := range out.Messages() {
				if m.Attempts != 0 && m.Attempts != tc.wantFailed {
					t.Fatalf("attempts = %d, want %d", m.Attempts, tc.wantFailed)
				}
			}
		})
	}
}

func TestWorkerNeverSendsChannelTurnedOff(t *testing.T) {
	ctx := context.Background()
	data := map[string]string{"number": "B-7"}
	tests := []struct {
		name      string
		pref      *notify.Preference // stored after Send queued the message
		wantEmail int
	}{
		{name: "no preference: sent", wantEmail: 1},
		{name: "turned off after queueing: not sent", pref: &notify.Preference{Enabled: false}, wantEmail: 0},
		{name: "explicitly on: sent", pref: &notify.Preference{Enabled: true}, wantEmail: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, out := newService(t)
			email := &fakeSender{}
			w := &notify.Worker{Service: s, Senders: map[notify.Channel]notify.Sender{notify.Email: email, notify.InApp: &fakeSender{}}}
			d := newDeliverer(out, w)
			if err := s.Send(ctx, nil, "u1", "billing.bill_ready", data); err != nil {
				t.Fatal(err)
			}
			if tc.pref != nil {
				p := *tc.pref
				p.UserID, p.Event, p.Channel = "u1", "billing.bill_ready", notify.Email
				if err := s.SetPreference(ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.Deliver(ctx, 10); err != nil {
				t.Fatal(err)
			}
			if len(email.got) != tc.wantEmail {
				t.Fatalf("email sends = %d, want %d", len(email.got), tc.wantEmail)
			}
			if p, _ := out.Pending(ctx, 10); len(p) != 0 {
				t.Fatalf("pending = %d, want 0 (a skipped channel is done, not retried)", len(p))
			}
		})
	}
}

func TestWorkerHandleErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		msg     outbox.Message
		senders map[notify.Channel]notify.Sender
		wantErr error
	}{
		{name: "other event type ignored", msg: outbox.Message{ID: "1", Type: "other.event", Payload: []byte(`{}`)}},
		{name: "unknown template", msg: outbox.Message{ID: "2", Type: notify.EventRequested, Payload: []byte(`{"to":"u1","template":"x.y","channel":"email"}`)}, wantErr: notify.ErrUnknownTemplate},
		{name: "unknown channel", msg: outbox.Message{ID: "3", Type: notify.EventRequested, Payload: []byte(`{"to":"u1","template":"billing.bill_ready","channel":"fax"}`)}, wantErr: notify.ErrUnknownChannel},
		{name: "no sender wired", msg: outbox.Message{ID: "4", Type: notify.EventRequested, Payload: []byte(`{"to":"u1","template":"billing.bill_ready","channel":"email"}`)}, wantErr: notify.ErrNoSender},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newService(t)
			w := &notify.Worker{Service: s, Senders: tc.senders}
			err := w.Handle(ctx, nil, tc.msg)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
