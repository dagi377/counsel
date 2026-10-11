package boot

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// partyRow scans its values into the destinations in order.
type partyRow []string

func (r partyRow) Scan(dest ...any) error {
	for i, v := range r {
		*dest[i].(*string) = v
	}
	return nil
}

// partyRows is the terms query result.
type partyRows struct {
	pgx.Rows
	terms []string
	at    int
}

func (r *partyRows) Next() bool             { r.at++; return r.at <= len(r.terms) }
func (r *partyRows) Scan(dest ...any) error { *dest[0].(*string) = r.terms[r.at-1]; return nil }
func (r *partyRows) Err() error             { return nil }
func (r *partyRows) Close()                 {}

// partyTx scripts ProposeParties: the touch returns the check's status and matter, each
// insert a new check_party id, and the terms query the check's terms.
type partyTx struct {
	pgx.Tx
	matterID string
	inserted int
}

func (t *partyTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.HasPrefix(sql, "UPDATE conflict.conflict_check") {
		return partyRow{"open", t.matterID}
	}
	t.inserted++
	return partyRow{"0192f000-0000-7000-8000-0000000000c" + string(rune('0'+t.inserted))}
}

func (t *partyTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &partyRows{terms: []string{"Northgate Rentals Inc."}}, nil
}

// rule: ARC-I-030
// Adding parties to a matter's conflict check publishes exactly one matter.party_added with
// ids only; a check on a lead only publishes nothing; with no emitter a matter's check is
// refused, so the event is never lost.
func TestMatterPartyAddedEvent(t *testing.T) {
	const (
		check = "0192f000-0000-7000-8000-0000000000a1"
		mid   = "0192f000-0000-7000-8000-0000000000b1"
		user  = "0192f000-0000-7000-8000-0000000000e1"
	)
	parties := []agent.ProposedParty{{Name: "Northgate Rentals Inc.", Role: "related", Kind: "org"}, {Name: "Avery Synth", Role: "witness"}}
	cases := []struct {
		name      string
		matterID  string
		noEmitter bool
		wantErr   error
		wantMsgs  int
	}{
		{"matter check publishes one event", mid, false, nil, 1},
		{"lead check publishes nothing", "", false, nil, 0},
		{"lead check needs no emitter", "", true, nil, 0},
		{"matter check without an emitter is refused", mid, true, agent.ErrMissingDep, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ob := outboxfake.New()
			s := PGConflictParties{DB: partyDB{&partyTx{matterID: tc.matterID}}, Events: ob}
			if tc.noEmitter {
				s.Events = nil
			}
			terms, err := s.ProposeParties(context.Background(), check, parties, user)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && !slices.Equal(terms, []string{"Northgate Rentals Inc."}) {
				t.Fatalf("terms = %v", terms)
			}
			msgs := ob.Messages()
			if len(msgs) != tc.wantMsgs {
				t.Fatalf("outbox = %d messages, want %d", len(msgs), tc.wantMsgs)
			}
			if tc.wantMsgs == 0 {
				return
			}
			m := msgs[0]
			if m.Type != matter.EventPartyAdded || m.Subject.Kind != "matter" || m.Subject.ID != mid {
				t.Fatalf("message = %s on %+v, want %s on matter %s", m.Type, m.Subject, matter.EventPartyAdded, mid)
			}
			var body map[string]any
			if err := json.Unmarshal(m.Payload, &body); err != nil {
				t.Fatal(err)
			}
			ids, _ := body["check_party_ids"].([]any)
			if body["matter_id"] != mid || body["conflict_check_id"] != check || body["added_by"] != user || len(ids) != len(parties) {
				t.Fatalf("payload = %s", m.Payload)
			}
			// No party name reaches the event.
			for _, p := range parties {
				if strings.Contains(string(m.Payload), p.Name) {
					t.Fatalf("payload carries a party name: %s", m.Payload)
				}
			}
		})
	}
}

// partyDB runs every transaction on one partyTx.
type partyDB struct{ tx *partyTx }

func (d partyDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (partyDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (partyDB) Ping(context.Context) error                     { return nil }
func (partyDB) Close()                                         {}
