package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Importing a bank-feed line publishes exactly one bank_feed.line_imported on the line; a
// body missing an id publishes nothing.
func TestPublishBankFeedLineImported(t *testing.T) {
	tests := []struct {
		name    string
		body    BankFeedLineImported
		want    int
		wantErr error
	}{
		{"line imported", BankFeedLineImported{LineID: "bl-1", AccountID: "acct-1", AmountCents: 125000, PostedOn: "2026-10-09"}, 1, nil},
		{"no line id", BankFeedLineImported{AccountID: "acct-1", PostedOn: "2026-10-09"}, 0, ErrInvalid},
		{"no posting day", BankFeedLineImported{LineID: "bl-1", AccountID: "acct-1"}, 0, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := PublishBankFeedLineImported(context.Background(), em, nil, tt.body); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			if tt.want == 1 && (msgs[0].Type != EventBankFeedLineImported || msgs[0].Subject.Kind != SubjectBankFeedLine || msgs[0].Subject.ID != "bl-1") {
				t.Fatalf("event = %s %+v", msgs[0].Type, msgs[0].Subject)
			}
		})
	}
}
