package timekeeping

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Each timekeeping publisher writes exactly one outbox event of its type, carrying ids and
// days only; a body missing an id publishes nothing.
func TestTimekeepingPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"bill draft requested", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishBillDraftRequested(ctx, em, nil, BillDraftRequested{MatterID: "m-1", UserID: "u-1", PeriodTo: "2026-09-30"})
		}, EventBillDraftRequested, "m-1", []string{"matter_id", "period_to", "user_id"}, nil},
		{"bill draft without matter", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishBillDraftRequested(ctx, em, nil, BillDraftRequested{UserID: "u-1"})
		}, "", "", nil, ErrInvalid},
		{"timesheet week submitted", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTimesheetWeekSubmitted(ctx, em, nil, TimesheetWeekSubmitted{UserID: "u-1", WeekStart: "2026-10-05"})
		}, EventTimesheetWeekSubmitted, "u-1:2026-10-05", []string{"user_id", "week_start"}, nil},
		{"timesheet without week", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishTimesheetWeekSubmitted(ctx, em, nil, TimesheetWeekSubmitted{UserID: "u-1"})
		}, "", "", nil, ErrInvalid},
		{"engagement letter signed", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishEngagementLetterSigned(ctx, em, nil, EngagementLetterSigned{MatterID: "m-1", DocumentID: "d-1", UserID: "u-1"})
		}, EventEngagementLetterSigned, "m-1", []string{"document_id", "matter_id", "user_id"}, nil},
		{"engagement letter without document", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishEngagementLetterSigned(ctx, em, nil, EngagementLetterSigned{MatterID: "m-1"})
		}, "", "", nil, ErrInvalid},
		{"rate change drafted", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishRateChangeDrafted(ctx, em, nil, RateChangeDrafted{RateChangeID: "rc-1", EffectiveFrom: "2027-01-01", UserID: "u-1"})
		}, EventRateChangeDrafted, "rc-1", []string{"effective_from", "rate_change_id", "user_id"}, nil},
		{"rate change without day", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishRateChangeDrafted(ctx, em, nil, RateChangeDrafted{RateChangeID: "rc-1"})
		}, "", "", nil, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := tt.publish(context.Background(), em); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if tt.wantErr != nil {
				if len(msgs) != 0 {
					t.Fatalf("events = %d, want 0", len(msgs))
				}
				return
			}
			if len(msgs) != 1 || msgs[0].Type != tt.typ || msgs[0].Subject.ID != tt.subject {
				t.Fatalf("events = %+v, want one %s on %s", msgs, tt.typ, tt.subject)
			}
			var body map[string]any
			if err := json.Unmarshal(msgs[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			var keys []string
			for k := range body {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, tt.keys) {
				t.Fatalf("payload keys = %v, want %v", keys, tt.keys)
			}
		})
	}
}
