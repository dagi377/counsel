package outboxfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox"
)

func TestFakeEmit(t *testing.T) {
	ctx := context.Background()
	s := outbox.Subject{Kind: "invoice", ID: "i1"}
	tests := []struct {
		name    string
		subject outbox.Subject
		typ     string
		body    any
		wantErr error
	}{
		{name: "ok", subject: s, typ: "invoice.issued", body: map[string]any{"id": "i1", "cents": 1200}},
		{name: "empty type", subject: s, body: map[string]any{}, wantErr: outbox.ErrInvalidEvent},
		{name: "empty subject", typ: "x", body: map[string]any{}, wantErr: outbox.ErrInvalidEvent},
		{name: "not an object", subject: s, typ: "x", body: []int{1}, wantErr: outbox.ErrInvalidEvent},
		{name: "unencodable", subject: s, typ: "x", body: map[string]any{"c": make(chan int)}, wantErr: outbox.ErrInvalidEvent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			err := f.Emit(ctx, nil, tt.subject, tt.typ, tt.body)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if want := map[bool]int{true: 1, false: 0}[tt.wantErr == nil]; len(f.Messages()) != want {
				t.Fatalf("messages = %d, want %d", len(f.Messages()), want)
			}
		})
	}
}

func TestFakeRelay(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		emit        int
		publish     int
		fail        string
		limit       int
		wantPending int
		wantErr     error
	}{
		{name: "all pending", emit: 3, wantPending: 3},
		{name: "limit", emit: 3, limit: 2, wantPending: 2},
		{name: "published drop out", emit: 3, publish: 2, wantPending: 1},
		{name: "unknown id", emit: 1, fail: "nope", wantPending: 1, wantErr: outbox.ErrNotFound},
		{name: "failed stays pending", emit: 1, fail: "msg-1", wantPending: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			for i := 0; i < tt.emit; i++ {
				if err := f.Emit(ctx, nil, outbox.Subject{Kind: "k", ID: "1"}, "t", map[string]int{"n": i}); err != nil {
					t.Fatal(err)
				}
			}
			for _, m := range f.Messages()[:tt.publish] {
				for range 2 { // second call is a no-op
					if err := f.MarkPublished(ctx, m.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tt.fail != "" {
				if err := f.MarkFailed(ctx, tt.fail); !errors.Is(err, tt.wantErr) {
					t.Fatalf("mark failed err = %v, want %v", err, tt.wantErr)
				}
			}
			got, err := f.Pending(ctx, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.wantPending {
				t.Fatalf("pending = %d, want %d", len(got), tt.wantPending)
			}
			if tt.fail == "msg-1" && got[0].Attempts != 1 {
				t.Fatalf("attempts = %d, want 1", got[0].Attempts)
			}
		})
	}
}

func TestFakeInboxClaim(t *testing.T) {
	ctx := context.Background()
	f := New()
	tests := []struct {
		name      string
		consumer  string
		id        string
		wantFirst bool
		wantErr   error
	}{
		{name: "first", consumer: "c1", id: "m1", wantFirst: true},
		{name: "repeat", consumer: "c1", id: "m1", wantFirst: false},
		{name: "other consumer", consumer: "c2", id: "m1", wantFirst: true},
		{name: "empty consumer", id: "m1", wantErr: outbox.ErrInvalidEvent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := f.Claim(ctx, nil, tt.consumer, tt.id)
			if !errors.Is(err, tt.wantErr) || first != tt.wantFirst {
				t.Fatalf("Claim = %v, %v; want %v, %v", first, err, tt.wantFirst, tt.wantErr)
			}
		})
	}
}
