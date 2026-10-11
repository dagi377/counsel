package work_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/work"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Each work publisher writes exactly one outbox event of its type; a body missing an id, or
// an automation event of another type, publishes nothing.
func TestWorkPublishers(t *testing.T) {
	run := work.AutomationRun{RunID: "ar-1", AutomationID: "auto-1", ReasonCode: "condition_false", UserID: "u-1"}
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		wantErr error
	}{
		{"automation run failed", func(ctx context.Context, em *outboxfake.Fake) error {
			return work.PublishAutomationRun(ctx, em, nil, work.EventAutomationRunFailed, run)
		}, work.EventAutomationRunFailed, "ar-1", nil},
		{"automation run skipped", func(ctx context.Context, em *outboxfake.Fake) error {
			return work.PublishAutomationRun(ctx, em, nil, work.EventAutomationRunSkipped, run)
		}, work.EventAutomationRunSkipped, "ar-1", nil},
		{"automation run of another type", func(ctx context.Context, em *outboxfake.Fake) error {
			return work.PublishAutomationRun(ctx, em, nil, "work.automation_run_done", run)
		}, "", "", work.ErrInvalidEvent},
		{"automation run without reason", func(ctx context.Context, em *outboxfake.Fake) error {
			return work.PublishAutomationRun(ctx, em, nil, work.EventAutomationRunFailed, work.AutomationRun{RunID: "ar-1", AutomationID: "auto-1"})
		}, "", "", work.ErrInvalidEvent},
		{"absence requested", func(ctx context.Context, em *outboxfake.Fake) error {
			return work.PublishAbsenceRequested(ctx, em, nil, work.AbsenceRequested{AbsenceID: "ab-1", UserID: "u-1", From: "2026-12-21", To: "2026-12-31"})
		}, work.EventAbsenceRequested, "ab-1", nil},
		{"absence without dates", func(ctx context.Context, em *outboxfake.Fake) error {
			return work.PublishAbsenceRequested(ctx, em, nil, work.AbsenceRequested{AbsenceID: "ab-1", UserID: "u-1"})
		}, "", "", work.ErrInvalidEvent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := tt.publish(context.Background(), em); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			want := 1
			if tt.wantErr != nil {
				want = 0
			}
			if len(msgs) != want {
				t.Fatalf("events = %d, want %d", len(msgs), want)
			}
			if want == 1 && (msgs[0].Type != tt.typ || msgs[0].Subject.ID != tt.subject) {
				t.Fatalf("event = %s %+v, want %s on %s", msgs[0].Type, msgs[0].Subject, tt.typ, tt.subject)
			}
		})
	}
}
