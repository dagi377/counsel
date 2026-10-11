package deadlines

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// wireRow scans fixed string values, or returns err.
type wireRow struct {
	vals []string
	err  error
}

func (r wireRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if p, ok := d.(*string); ok && i < len(r.vals) {
			*p = r.vals[i]
		}
	}
	return nil
}

// wireTx answers QueryRow with row. Other pgx.Tx methods panic.
type wireTx struct {
	pgx.Tx
	row wireRow
}

func (t *wireTx) QueryRow(context.Context, string, ...any) pgx.Row { return t.row }

// wireDB hands its tx to every InTx.
type wireDB struct{ tx *wireTx }

func (d wireDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (d wireDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (d wireDB) Ping(context.Context) error                     { return nil }
func (d wireDB) Close()                                         {}

// rule: ARC-I-030
// Adding a calendar event publishes exactly one calendar_event.added in its tx.
func TestCalendarEventAddedPublishesOneEvent(t *testing.T) {
	at := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)
	ok := NewCalendarEvent{CalendarID: "cal1", Title: "Meeting", Type: "meeting", StartsAt: at, EndsAt: at.Add(time.Hour), MatterID: "m1"}
	bad := ok
	bad.Type = "deadline"
	tests := []struct {
		name    string
		in      NewCalendarEvent
		want    int
		wantErr error
	}{
		{"meeting on a matter", ok, 1, nil},
		{"deadline events come from the engine", bad, 0, ErrInvalidEvent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			c := &CalendarEvents{DB: wireDB{tx: &wireTx{row: wireRow{vals: []string{"e1"}}}}, Events: em}
			_, err := c.Add(context.Background(), tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			if tt.want == 1 && (msgs[0].Type != EventCalendarEventAdded || msgs[0].Subject.ID != "e1") {
				t.Fatalf("event = %s %+v", msgs[0].Type, msgs[0].Subject)
			}
		})
	}
}
