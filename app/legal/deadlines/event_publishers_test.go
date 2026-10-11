package deadlines

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Each publisher writes exactly one outbox event of its type with ids-only keys; a body
// missing a required id publishes nothing (EVENTS-9).
func TestARC_I_030_DeadlinesEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"calendar.event_cancelled", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCalendarEventCancelled(ctx, em, nil, CalendarEventCancelled{CalendarEventID: "calendar-event-id-1", MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventCalendarEventCancelled, "calendar-event-id-1", []string{"calendar_event_id", "matter_id", "user_id"}, nil},
		{"calendar.event_cancelled without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCalendarEventCancelled(ctx, em, nil, CalendarEventCancelled{})
		}, "", "", nil, ErrEventBody},
		{"calendar.event_ended", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCalendarEventEnded(ctx, em, nil, CalendarEventEnded{CalendarEventID: "calendar-event-id-1", MatterID: "matter-id-1", UserID: "user-id-1"})
		}, EventCalendarEventEnded, "calendar-event-id-1", []string{"calendar_event_id", "matter_id", "user_id"}, nil},
		{"calendar.event_ended without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCalendarEventEnded(ctx, em, nil, CalendarEventEnded{})
		}, "", "", nil, ErrEventBody},
		{"calendar.event_moved", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCalendarEventMoved(ctx, em, nil, CalendarEventMoved{CalendarEventID: "calendar-event-id-1", MatterID: "matter-id-1", FromDay: "from-day-1", ToDay: "to-day-1", UserID: "user-id-1"})
		}, EventCalendarEventMoved, "calendar-event-id-1", []string{"calendar_event_id", "from_day", "matter_id", "to_day", "user_id"}, nil},
		{"calendar.event_moved without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishCalendarEventMoved(ctx, em, nil, CalendarEventMoved{})
		}, "", "", nil, ErrEventBody},
		{"work.absence_approved", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishAbsenceApproved(ctx, em, nil, AbsenceApproved{AbsenceID: "absence-id-1", AbsentUserID: "absent-user-id-1", From: "from-1", To: "to-1", UserID: "user-id-1"})
		}, EventAbsenceApproved, "absence-id-1", []string{"absence_id", "absent_user_id", "from", "to", "user_id"}, nil},
		{"work.absence_approved without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishAbsenceApproved(ctx, em, nil, AbsenceApproved{})
		}, "", "", nil, ErrEventBody},
		{"deadline.met", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDeadlineMet(ctx, em, nil, DeadlineMet{DeadlineID: "deadline-id-1", GateID: "gate-id-1", UserID: "user-id-1"})
		}, EventDeadlineMet, "deadline-id-1", []string{"deadline_id", "gate_id", "user_id"}, nil},
		{"deadline.met without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishDeadlineMet(ctx, em, nil, DeadlineMet{})
		}, "", "", nil, ErrEventBody},
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
