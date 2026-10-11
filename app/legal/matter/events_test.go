package matter

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
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
		{name: "note added", row: stubRow{vals: []any{"n1"}}, want: EventNoteAdded,
			write: func(ctx context.Context, s *Store) error {
				_, err := s.AddNote(ctx, NewNote{MatterID: "m1", AuthorID: "u1", BodyRef: "obj/n1"})
				return err
			}},
		{name: "opening gate effect", row: stubRow{vals: []any{"2026-0001", "c1", "pa1", "u1"}}, want: EventOpened,
			write: func(ctx context.Context, s *Store) error {
				clk, err := clock.New("America/Toronto")
				if err != nil {
					return err
				}
				return s.OpeningEffect(clk)(ctx, &stubTx{row: stubRow{vals: []any{"2026-0001", "c1", "pa1", "u1"}}}, registry.Approved{
					GateID: "g1", Kind: GateOpening, Payload: []byte(`{"matter_id":"m1","status":"open"}`),
					Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: "u3"}}})
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

// rule: ARC-I-016
// rule: ARC-I-017
// rule: ARC-I-030
// The opening effect opens a pending matter or creates the matter already open, in one
// approval, publishing matter.opened once; the agent and the preparer never approve, and any
// other payload is refused with nothing published.
func TestOpeningEffect(t *testing.T) {
	clk, err := clock.New("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "u3"}
	preparer := registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
	const create = `{"matter_id":"0190c000-0000-4000-8000-0000000000aa","number":"2026-0042","short_name":"Delta",` +
		`"client_id":"c1","practice_area_id":"pa1","practice_area":"pa1","responsible_lawyer_id":"u9"}`
	tests := []struct {
		name      string
		payload   string
		preparer  registry.Principal
		approvers []registry.Principal
		ok        bool
		wantArgs  int // SQL arguments of the write: 3 for open, 10 for create-and-open
	}{
		{"status change opens", `{"matter_id":"m1","status":"open"}`, preparer, []registry.Principal{lawyer}, true, 3},
		{"create shape creates and opens", create, preparer, []registry.Principal{lawyer}, true, 10},
		{"agent-prepared, another lawyer approves", create,
			registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u1"}, []registry.Principal{lawyer}, true, 10},
		{"agent approver refused", create, preparer, []registry.Principal{{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u3"}}, false, 0},
		{"preparer approving refused", create, preparer, []registry.Principal{preparer}, false, 0},
		{"user the agent acted for approving refused", create,
			registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u3"}, []registry.Principal{lawyer}, false, 0},
		{"no approver refused", create, preparer, nil, false, 0},
		{"create shape without number refused", `{"matter_id":"m1","short_name":"Delta","client_id":"c1","practice_area_id":"pa1","responsible_lawyer_id":"u9"}`,
			preparer, []registry.Principal{lawyer}, false, 0},
		{"no matter refused", `{"client_id":"c1"}`, preparer, []registry.Principal{lawyer}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore(&stubDB{tx: &stubTx{}}, nil)
			em := outboxfake.New()
			s.Events = em
			tx := &stubTx{row: stubRow{vals: []any{"2026-0042", "c1", "pa1", "u9"}}}
			err := s.OpeningEffect(clk)(context.Background(), tx, registry.Approved{GateID: "g1", Kind: GateOpening,
				Payload: []byte(tt.payload), Preparer: tt.preparer, Approvers: tt.approvers})
			if (err == nil) != tt.ok {
				t.Fatalf("err = %v, want ok %v", err, tt.ok)
			}
			want := 0
			if tt.ok {
				want = 1
			}
			if n := len(em.Messages()); n != want || len(tx.args) != tt.wantArgs {
				t.Fatalf("events = %d args = %d, want %d and %d", n, len(tx.args), want, tt.wantArgs)
			}
			if tt.ok && em.Messages()[0].Type != EventOpened {
				t.Fatalf("event = %s", em.Messages()[0].Type)
			}
		})
	}
}
