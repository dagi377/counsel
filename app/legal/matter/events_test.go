package matter

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Opening a matter and changing its stage each publish exactly one event in the write's tx.
func TestMatterWritesPublishOneEvent(t *testing.T) {
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		row   stubRow
		write func(context.Context, *Store) error
		want  string
	}{
		{name: "open", row: stubRow{vals: []any{"2026-0001", "c1", "pa1", "u1"}}, want: EventOpened,
			write: func(ctx context.Context, s *Store) error {
				return s.Open(ctx, "m1", clock.Date{Year: 2026, Month: 10, Day: 11}, "u2")
			}},
		{name: "stage change", row: stubRow{vals: []any{"sc1"}}, want: EventStageChanged,
			write: func(ctx context.Context, s *Store) error {
				return s.ChangeStage(ctx, StageChange{MatterID: "m1", FromStageID: "s1", ToStageID: "s2", ApprovedBy: "u2", At: at}, "")
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			s := NewStore(&stubDB{tx: &stubTx{row: tt.row}}, nil)
			s.Events = em
			if err := tt.write(context.Background(), s); err != nil {
				t.Fatalf("write: %v", err)
			}
			msgs := em.Messages()
			if len(msgs) != 1 {
				t.Fatalf("events = %d, want 1", len(msgs))
			}
			if m := msgs[0]; m.Type != tt.want || m.Subject.Kind != matterkind.Subject || m.Subject.ID != "m1" {
				t.Fatalf("event = %s %+v", m.Type, m.Subject)
			}
		})
	}
}

// Without an emitter the writes refuse, so no change commits without its event.
func TestMatterWritesNeedEmitter(t *testing.T) {
	s := NewStore(&stubDB{tx: &stubTx{}}, nil)
	if err := s.Open(context.Background(), "m1", clock.Date{Year: 2026, Month: 10, Day: 11}, "u2"); err == nil {
		t.Fatal("Open without emitter succeeded")
	}
}
