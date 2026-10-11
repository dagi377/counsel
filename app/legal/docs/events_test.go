package docs

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
// Each document write point publishes exactly one event in its tx; a no-op write publishes none.
func TestDocumentWritePointsPublishOneEvent(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	form := CourtFormVersion{RuleSet: "RCP", FormNumber: "14A", Title: "Statement of claim",
		SourceURL: "https://example.test/14a", VersionDate: day, Checksum: []byte{1}, CheckedAt: day}
	tests := []struct {
		name  string
		row   wireRow
		write func(context.Context, db.DB, *outboxfake.Fake) error
		want  []string
		subj  string
	}{
		{name: "revision requested", row: wireRow{vals: []string{"m1"}}, want: []string{EventRevisionRequested}, subj: "d1",
			write: func(ctx context.Context, d db.DB, em *outboxfake.Fake) error {
				return (&Revisions{DB: d, Events: em}).Request(ctx, "d1", "v1", "u1")
			}},
		{name: "court form changed", row: wireRow{vals: []string{"cf1"}}, want: []string{EventCourtFormChanged}, subj: "cf1",
			write: func(ctx context.Context, d db.DB, em *outboxfake.Fake) error {
				_, err := (&CourtForms{DB: d, Events: em}).Record(ctx, form)
				return err
			}},
		{name: "court form already recorded", row: wireRow{err: pgx.ErrNoRows},
			write: func(ctx context.Context, d db.DB, em *outboxfake.Fake) error {
				_, err := (&CourtForms{DB: d, Events: em}).Record(ctx, form)
				return err
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := tt.write(context.Background(), wireDB{tx: &wireTx{row: tt.row}}, em); err != nil {
				t.Fatalf("write: %v", err)
			}
			msgs := em.Messages()
			if len(msgs) != len(tt.want) {
				t.Fatalf("events = %d, want %d", len(msgs), len(tt.want))
			}
			for i, m := range msgs {
				if m.Type != tt.want[i] || m.Subject.ID != tt.subj {
					t.Fatalf("event = %s %+v", m.Type, m.Subject)
				}
			}
		})
	}
}

// Without an emitter the writes refuse, so nothing commits without its event.
func TestDocumentWritePointsNeedEmitter(t *testing.T) {
	err := (&Revisions{DB: wireDB{tx: &wireTx{}}}).Request(context.Background(), "d1", "v1", "u1")
	if !errors.Is(err, ErrNoWritePoint) {
		t.Fatalf("err = %v, want ErrNoWritePoint", err)
	}
}
