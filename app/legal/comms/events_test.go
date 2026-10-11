package comms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

func TestMessageEventsPublishOne(t *testing.T) {
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	ok := MessageEvent{CommItemID: "c-1", MatterID: "m-1", OccurredAt: at}
	tests := []struct {
		name    string
		publish func(MessageEvents, context.Context, pgx.Tx, MessageEvent) error
		typ     string
		body    MessageEvent
		wantErr error
	}{
		{"email needs reply", MessageEvents.NeedsReply, EventEmailNeedsReply, ok, nil},
		{"text received", MessageEvents.TextReceived, EventTextReceived, ok, nil},
		{"portal message received", MessageEvents.PortalMessageReceived, EventPortalMessageReceived, ok, nil},
		{"no comm item", MessageEvents.TextReceived, EventTextReceived, MessageEvent{OccurredAt: at}, ErrInvalid},
		{"no time", MessageEvents.NeedsReply, EventEmailNeedsReply, MessageEvent{CommItemID: "c-1"}, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := outboxfake.New()
			err := tt.publish(MessageEvents{Emitter: f}, context.Background(), nil, tt.body)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := f.Messages()
			if tt.wantErr != nil {
				if len(msgs) != 0 {
					t.Fatalf("published %d events on error", len(msgs))
				}
				return
			}
			// rule: ARC-I-030
			if len(msgs) != 1 || msgs[0].Type != tt.typ || msgs[0].Subject.ID != "c-1" {
				t.Fatalf("messages = %+v, want one %s on c-1", msgs, tt.typ)
			}
		})
	}
	if err := (MessageEvents{}).TextReceived(context.Background(), nil, ok); !errors.Is(err, ErrNoWritePoint) {
		t.Fatalf("no emitter: err = %v, want ErrNoWritePoint", err)
	}
}
