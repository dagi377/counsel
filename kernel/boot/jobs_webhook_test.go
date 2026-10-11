package boot

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/webhook"
	"github.com/dagi377/counsel/kernel/webhook/webhookfake"
)

// endpointList is a fixed WebhookEndpoints.
type endpointList []string

func (l endpointList) Endpoints(context.Context) ([]string, error) { return l, nil }

// recordingPoster records every post as "<webhook-id> <subject id>" and fails the attempts
// listed in fail (by post number, from 1).
type recordingPoster struct {
	posts []string
	fail  map[int]bool
}

var errEndpoint500 = errors.New("endpoint answered 500")

func (p *recordingPoster) Post(_ context.Context, d webhook.Delivery) error {
	p.posts = append(p.posts, d.Message.ID+" "+d.Message.Subject.ID)
	if p.fail[len(p.posts)] {
		return errEndpoint500
	}
	return nil
}

func TestAddWebhookJobsKinds(t *testing.T) {
	tests := []struct {
		name       string
		dispatcher bool
		poster     bool
		wantKinds  []string
		wantErr    error
	}{
		{name: "nil dispatcher -> kind absent", wantKinds: []string{}},
		{name: "dispatcher -> webhook.deliver", dispatcher: true, poster: true, wantKinds: []string{KindWebhookDeliver}},
		{name: "dispatcher without poster -> config error", dispatcher: true, wantErr: ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ob := outboxfake.New()
			k := &Kernel{Jobs: NewJobs(), Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob}}
			if tt.dispatcher {
				k.Dispatcher = webhookfake.New()
				k.WebhookEndpoints = endpointList{"ep1"}
			}
			if tt.poster {
				k.WebhookPoster = &recordingPoster{}
			}
			err := addWebhookJobs(k)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("addWebhookJobs() error = %v, want %v", err, tt.wantErr)
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
		})
	}
}

func TestBuildWithNilDispatcher(t *testing.T) {
	k, err := BuildWith(context.Background(), Deps{}, nil, App{Name: "a", Register: func(context.Context, *Kernel) error { return nil }})
	if err != nil {
		t.Fatalf("BuildWith() = %v", err)
	}
	if err := addWebhookJobs(k); err != nil {
		t.Fatalf("addWebhookJobs() = %v", err)
	}
	for _, kind := range k.Jobs.Kinds() {
		if kind == KindWebhookDeliver {
			t.Fatalf("Kinds() has %s with no dispatcher", kind)
		}
	}
}

// TestARC_I_034_WebhookDeliverOrder: messages for one subject go out in emit order; a failed
// delivery holds its subject back, keeps its webhook-id on retry, and other subjects go on.
func TestARC_I_034_WebhookDeliverOrder(t *testing.T) {
	type emit struct{ subject string }
	tests := []struct {
		name  string
		emits []emit
		fail  map[int]bool
		runs  int
		want  []string // "<webhook-id> <subject id>" in post order
	}{
		{name: "two messages for one subject in order", emits: []emit{{"s1"}, {"s1"}}, runs: 1,
			want: []string{"msg-1 s1", "msg-2 s1"}},
		// rule: ARC-I-034
		{name: "failed delivery retried with the same webhook-id before the next", emits: []emit{{"s1"}, {"s1"}},
			fail: map[int]bool{1: true}, runs: 2, want: []string{"msg-1 s1", "msg-1 s1", "msg-2 s1"}},
		{name: "a failing subject does not hold back another", emits: []emit{{"s1"}, {"s1"}, {"s2"}},
			fail: map[int]bool{1: true}, runs: 1, want: []string{"msg-1 s1", "msg-3 s2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ob := outboxfake.New()
			disp := webhookfake.New()
			if err := disp.RegisterType(webhook.EventType{Name: "desk.signed", Schema: []byte(`{"type":"object"}`)}); err != nil {
				t.Fatal(err)
			}
			if err := disp.AddEndpoint(ctx, webhook.Endpoint{ID: "ep1", URL: "https://hooks.invalid/ep1"}); err != nil {
				t.Fatal(err)
			}
			poster := &recordingPoster{fail: tt.fail}
			k := &Kernel{Jobs: NewJobs(), DB: dbfake.New(), Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob},
				Dispatcher: disp, WebhookEndpoints: endpointList{"ep1"}, WebhookPoster: poster}
			if err := addKernelJobs(k); err != nil {
				t.Fatal(err)
			}
			if err := addWebhookJobs(k); err != nil {
				t.Fatal(err)
			}
			for _, e := range tt.emits {
				if err := ob.Emit(ctx, nil, outbox.Subject{Kind: "desk", ID: e.subject}, "desk.signed", map[string]int{"n": 1}); err != nil {
					t.Fatal(err)
				}
			}
			// A non-webhook event on the outbox is not queued and does not stop the relay.
			if err := ob.Emit(ctx, nil, outbox.Subject{Kind: "desk", ID: "s9"}, "desk.internal", map[string]int{"n": 1}); err != nil {
				t.Fatal(err)
			}
			if err := (outboxRelayWorker{k: k}).Work(ctx, &river.Job[outboxRelayArgs]{}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < tt.runs; i++ {
				if err := (webhookDeliverWorker{k: k}).Work(ctx, &river.Job[webhookDeliverArgs]{}); err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
			}
			if !reflect.DeepEqual(poster.posts, tt.want) {
				t.Fatalf("posts = %v, want %v", poster.posts, tt.want)
			}
		})
	}
}
