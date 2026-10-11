package batch

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Asking for a billing cycle publishes exactly one billing.cycle_requested on the batch run;
// a body missing an id publishes nothing.
func TestPublishCycleRequested(t *testing.T) {
	tests := []struct {
		name    string
		body    CycleRequested
		want    int
		wantErr error
	}{
		{"cycle requested", CycleRequested{RunID: "run-1", PeriodTo: "2026-09-30", UserID: "u-1"}, 1, nil},
		{"no run id", CycleRequested{PeriodTo: "2026-09-30", UserID: "u-1"}, 0, ErrInvalid},
		{"no user", CycleRequested{RunID: "run-1", PeriodTo: "2026-09-30"}, 0, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := PublishCycleRequested(context.Background(), em, nil, tt.body); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			if tt.want == 1 && (msgs[0].Type != EventCycleRequested || msgs[0].Subject.Kind != SubjectCycle || msgs[0].Subject.ID != "run-1") {
				t.Fatalf("event = %s %+v", msgs[0].Type, msgs[0].Subject)
			}
		})
	}
}
