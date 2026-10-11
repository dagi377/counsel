package webhookfake

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/webhook"
)

func TestSignVerify(t *testing.T) {
	// Standard Webhooks reference vector.
	secret, _ := base64.StdEncoding.DecodeString("MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw")
	ts := time.Unix(1614265330, 0)
	body := []byte(`{"test": 2432232314}`)
	const msgID = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	const want = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
	f := New()
	if got := f.Sign(secret, msgID, ts, body); got != want {
		t.Fatalf("Sign = %q, want %q", got, want)
	}
	tests := []struct {
		name   string
		header string
		now    time.Time
		body   []byte
		err    error
	}{
		{name: "valid", header: want, now: ts, body: body},
		{name: "valid among several", header: "v1,bm9wZQ== " + want, now: ts.Add(time.Minute), body: body},
		{name: "tampered body", header: want, now: ts, body: []byte(`{"test": 1}`), err: webhook.ErrBadSignature},
		{name: "old timestamp", header: want, now: ts.Add(10 * time.Minute), body: body, err: webhook.ErrTimestampSkew},
		{name: "future timestamp", header: want, now: ts.Add(-10 * time.Minute), body: body, err: webhook.ErrTimestampSkew},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.Verify(secret, msgID, ts, tt.now, tt.body, tt.header); !errors.Is(err, tt.err) {
				t.Fatalf("Verify err = %v, want %v", err, tt.err)
			}
		})
	}
}

func msg(id, typ, subj string) webhook.Message {
	return webhook.Message{ID: id, Type: typ, Subject: webhook.Subject{Kind: "ticket", ID: subj}, Payload: json.RawMessage(`{}`)}
}

func TestEnqueue(t *testing.T) {
	tests := []struct {
		name  string
		m     webhook.Message
		wantN int
		err   error
	}{
		{name: "registered type to subscribers", m: msg("1", "ticket.created", "a"), wantN: 2},
		{name: "type only one endpoint wants", m: msg("2", "ticket.closed", "a"), wantN: 1},
		{name: "unregistered type refused", m: msg("3", "ticket.secret", "a"), err: webhook.ErrUnknownType},
		{name: "bad payload", m: webhook.Message{Type: "ticket.created", Payload: json.RawMessage(`{`)}, err: webhook.ErrInvalidPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New()
			for _, ty := range []string{"ticket.created", "ticket.closed"} {
				if err := f.RegisterType(webhook.EventType{Name: ty}); err != nil {
					t.Fatal(err)
				}
			}
			_ = f.AddEndpoint(ctx, webhook.Endpoint{ID: "e1"})
			_ = f.AddEndpoint(ctx, webhook.Endpoint{ID: "e2", Types: []string{"ticket.created"}})
			ids, err := f.Enqueue(ctx, nil, tt.m)
			if !errors.Is(err, tt.err) || len(ids) != tt.wantN {
				t.Fatalf("Enqueue = %v, %v; want %d ids, %v", ids, err, tt.wantN, tt.err)
			}
		})
	}
	if err := New().RegisterType(webhook.EventType{Name: ""}); !errors.Is(err, webhook.ErrUnknownType) {
		t.Fatalf("empty type err = %v", err)
	}
}

func TestOrderedPerSubject(t *testing.T) {
	ctx := context.Background()
	f := New()
	_ = f.RegisterType(webhook.EventType{Name: "ticket.updated"})
	if err := f.RegisterType(webhook.EventType{Name: "ticket.updated"}); !errors.Is(err, webhook.ErrDuplicateType) {
		t.Fatalf("duplicate err = %v", err)
	}
	_ = f.AddEndpoint(ctx, webhook.Endpoint{ID: "e1"})
	var ids []webhook.DeliveryID
	for _, m := range []webhook.Message{msg("1", "ticket.updated", "a"), msg("2", "ticket.updated", "a"), msg("3", "ticket.updated", "b")} {
		got, err := f.Enqueue(ctx, nil, m)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, got...)
	}
	tests := []struct {
		name string
		act  func() error
		want []string
	}{
		{name: "heads only", want: []string{"1", "3"}},
		{name: "failed head stays", act: func() error { return f.Fail(ctx, ids[0]) }, want: []string{"1", "3"}},
		{name: "ack releases next", act: func() error { return f.Ack(ctx, ids[0]) }, want: []string{"2", "3"}},
		{name: "all acked", act: func() error {
			if err := f.Ack(ctx, ids[1]); err != nil {
				return err
			}
			return f.Ack(ctx, ids[2])
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.act != nil {
				if err := tt.act(); err != nil {
					t.Fatal(err)
				}
			}
			ready, err := f.Ready(ctx, "e1")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, d := range ready {
				got = append(got, d.Message.ID)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Ready = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Ready = %v, want %v", got, tt.want)
				}
			}
		})
	}
	if _, err := f.Ready(ctx, "nope"); !errors.Is(err, webhook.ErrUnknownEndpoint) {
		t.Fatalf("Ready unknown err = %v", err)
	}
	if err := f.Ack(ctx, "nope"); !errors.Is(err, webhook.ErrNotFound) {
		t.Fatalf("Ack unknown err = %v", err)
	}
}
