package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/jackc/pgx/v5"
)

type memRecords map[string][]Record

func (m memRecords) Records(_ context.Context, class string) ([]Record, error) { return m[class], nil }

type memLists struct{ lists map[string]PurgeList }

func (m *memLists) InsertList(_ context.Context, l PurgeList) error {
	if _, ok := m.lists[l.ID]; ok {
		return errors.New("duplicate list")
	}
	m.lists[l.ID] = l
	return nil
}

func (m *memLists) List(_ context.Context, _ pgx.Tx, id string) (PurgeList, error) {
	l, ok := m.lists[id]
	if !ok {
		return PurgeList{}, ErrUnknownList
	}
	return l, nil
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, Toronto) }

func newPurger(t *testing.T, destroyed *[]string, gates map[string]registry.Approved) *Purger {
	t.Helper()
	reg := NewRegistry()
	for _, c := range []Class{
		{ID: "files", MinYears: 7, DestructionGate: "records.destroy"},
		{ID: "ledger", NeverPurge: true},
	} {
		if err := reg.RegisterClass(c); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	return &Purger{
		Classes: reg,
		Records: memRecords{
			"files": {
				{ID: "old", Class: "files", ClosedOn: day(2018, 1, 1)},
				{ID: "held", Class: "files", ClosedOn: day(2018, 1, 1), Hold: true},
				{ID: "young", Class: "files", ClosedOn: day(2024, 1, 1)},
			},
			"ledger": {{ID: "ledger-1", Class: "ledger", ClosedOn: day(1990, 1, 1)}},
		},
		Lists: &memLists{lists: map[string]PurgeList{}},
		Gates: func(_ context.Context, _ pgx.Tx, id string) (registry.Approved, error) {
			g, ok := gates[id]
			if !ok {
				return registry.Approved{}, errors.New("gate not approved")
			}
			return g, nil
		},
		Destroy: func(_ context.Context, _ pgx.Tx, _ string, ids []string) error {
			*destroyed = append(*destroyed, ids...)
			return nil
		},
		Now:   func() time.Time { return day(2026, 10, 11) },
		NewID: func() string { n++; return fmt.Sprintf("list-%d", n) },
	}
}

func payload(id string) json.RawMessage { b, _ := json.Marshal(PurgePayload{ListID: id}); return b }

func TestProposeOnlyInserts(t *testing.T) {
	tests := []struct {
		class   string
		want    []string
		wantErr error
	}{
		{"files", []string{"old"}, nil},
		{"ledger", nil, ErrNeverPurge},
		{"nope", nil, ErrUnknownClass},
	}
	for _, tc := range tests {
		t.Run(tc.class, func(t *testing.T) {
			var destroyed []string
			p := newPurger(t, &destroyed, nil)
			l, err := p.Propose(context.Background(), tc.class)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Propose err = %v, want %v", err, tc.wantErr)
			}
			if len(destroyed) != 0 {
				t.Errorf("Propose destroyed %v", destroyed)
			}
			stored := p.Lists.(*memLists).lists
			if tc.wantErr != nil {
				if len(stored) != 0 {
					t.Errorf("refused class stored a list: %v", stored)
				}
				return
			}
			if fmt.Sprint(l.Records) != fmt.Sprint(tc.want) || fmt.Sprint(stored[l.ID].Records) != fmt.Sprint(tc.want) {
				t.Errorf("records = %v stored %v, want %v", l.Records, stored[l.ID], tc.want)
			}
		})
	}
}

func TestApplyOnlyThroughDestructionGate(t *testing.T) {
	gates := map[string]registry.Approved{
		"g-ok":    {GateID: "g-ok", Kind: "records.destroy", Payload: payload("list-1")},
		"g-kind":  {GateID: "g-kind", Kind: "billing.write_off", Payload: payload("list-1")},
		"g-other": {GateID: "g-other", Kind: "records.destroy", Payload: payload("list-9")},
	}
	tests := []struct {
		name    string
		gate    string
		effect  bool
		wantErr error
	}{
		{"approved gate", "g-ok", false, nil},
		{"effect handler", "g-ok", true, nil},
		{"unapproved gate", "g-missing", false, ErrNotApproved},
		{"wrong gate kind", "g-kind", false, ErrNotApproved},
		{"gate for another list", "g-other", false, ErrNotApproved},
		{"effect wrong kind", "g-kind", true, ErrNotApproved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var destroyed []string
			p := newPurger(t, &destroyed, gates)
			if _, err := p.Propose(context.Background(), "files"); err != nil {
				t.Fatal(err)
			}
			var err error
			if tc.effect {
				err = p.Effect()(context.Background(), nil, gates[tc.gate])
			} else {
				err = p.Apply(context.Background(), nil, "list-1", tc.gate)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			want := "[]"
			if tc.wantErr == nil {
				want = "[old]"
			}
			if fmt.Sprint(destroyed) != want {
				t.Errorf("destroyed = %v, want %s", destroyed, want)
			}
		})
	}
}
