package comms

import (
	"context"
	"encoding/json"
	"strings"
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
// An inbound email publishes exactly one email.received in its tx; a redelivery publishes none.
func TestInboundEmailPublishesOneEvent(t *testing.T) {
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	mail := InboundEmail{ConnectorID: "c1", ExternalID: "x1", Participants: json.RawMessage(`[]`), BodyRef: "obj/1", OccurredAt: at}
	tests := []struct {
		name   string
		row    wireRow
		want   int
		wantID string
	}{
		{"new email", wireRow{vals: []string{"ci1"}}, 1, "ci1"},
		{"redelivered email", wireRow{err: pgx.ErrNoRows}, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			s := &Inbound{DB: wireDB{tx: &wireTx{row: tt.row}}, Events: em}
			id, err := s.Receive(context.Background(), mail)
			if err != nil || id != tt.wantID {
				t.Fatalf("Receive = %q, %v", id, err)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			if tt.want == 1 && (msgs[0].Type != EventEmailReceived || msgs[0].Subject.ID != "ci1") {
				t.Fatalf("event = %s %+v", msgs[0].Type, msgs[0].Subject)
			}
		})
	}
}

// rule: ARC-I-030
// email.received carries the to, cc and bcc addresses (lower-cased, de-duplicated) in its
// additive "to" field so F-INT-02 can ignore mail not sent to the intake address; one event.
func TestInboundEmailReceivedTo(t *testing.T) {
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		participants string
		want         []string
	}{
		{"to and cc", `[{"role":"from","address":"p@x.test"},{"role":"to","address":"Intake@Firm.test"},{"role":"cc","email":"b@firm.test"}]`,
			[]string{"intake@firm.test", "b@firm.test"}},
		{"duplicate and bcc", `[{"role":"to","address":"a@firm.test"},{"role":"bcc","address":"A@firm.test"}]`,
			[]string{"a@firm.test"}},
		{"no recipients", `[{"role":"from","address":"p@x.test"}]`, []string{}},
		{"empty", `[]`, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			s := &Inbound{DB: wireDB{tx: &wireTx{row: wireRow{vals: []string{"ci1"}}}}, Events: em}
			mail := InboundEmail{ConnectorID: "c1", ExternalID: "x1", Participants: json.RawMessage(tt.participants),
				BodyRef: "obj/1", OccurredAt: at}
			if _, err := s.Receive(context.Background(), mail); err != nil {
				t.Fatalf("Receive: %v", err)
			}
			msgs := em.Messages()
			if len(msgs) != 1 || msgs[0].Type != EventEmailReceived {
				t.Fatalf("events = %d, want one %s", len(msgs), EventEmailReceived)
			}
			var body struct {
				To []string `json:"to"`
			}
			if err := json.Unmarshal(msgs[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			if body.To == nil || strings.Join(body.To, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("to = %#v, want %#v", body.To, tt.want)
			}
		})
	}
}
