package retention_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/retention"
)

type countEngine struct{ proposed int }

func (c *countEngine) Propose(_ context.Context, class string) (retention.PurgeList, error) {
	c.proposed++
	return retention.PurgeList{ID: "l1", Class: class}, nil
}

func (c *countEngine) Apply(context.Context, pgx.Tx, string, string) error { return nil }

func guardRegistry(t *testing.T) *retention.Registry {
	t.Helper()
	g := retention.NewRegistry()
	for _, c := range []retention.Class{
		{ID: "ledger_book", NeverPurge: true},
		{ID: "statement_book", NeverPurge: true},
		{ID: "invoice_book", NeverPurge: true},
		{ID: "long_record", MinYears: 6, DestructionGate: "record_destroy"},
	} {
		if err := g.RegisterClass(c); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func TestGuardCheckClass(t *testing.T) {
	g := retention.Guard{Classes: guardRegistry(t)}
	// rule: ARC-U-013
	tests := []struct {
		class string
		want  error
	}{
		{"ledger_book", retention.ErrNeverPurge},
		{"statement_book", retention.ErrNeverPurge},
		{"invoice_book", retention.ErrNeverPurge},
		{"long_record", nil},
		{"unregistered", retention.ErrUnknownClass},
	}
	for _, tc := range tests {
		t.Run(tc.class, func(t *testing.T) {
			if err := g.CheckClass(tc.class); !errors.Is(err, tc.want) {
				t.Fatalf("CheckClass = %v, want %v", err, tc.want)
			}
			if err := g.CheckList(retention.PurgeList{ID: "l", Class: tc.class}); !errors.Is(err, tc.want) {
				t.Fatalf("CheckList = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGuardCheckRecords(t *testing.T) {
	g := retention.Guard{Classes: guardRegistry(t)}
	// rule: ARC-U-013
	tests := []struct {
		name string
		recs []retention.Record
		want error
	}{
		{"purgeable only", []retention.Record{{ID: "a", Class: "long_record"}}, nil},
		{"empty", nil, nil},
		{"one protected in a mixed batch", []retention.Record{{ID: "a", Class: "long_record"}, {ID: "b", Class: "ledger_book"}}, retention.ErrNeverPurge},
		{"unknown class", []retention.Record{{ID: "a", Class: "x"}}, retention.ErrUnknownClass},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := g.CheckRecords(tc.recs); !errors.Is(err, tc.want) {
				t.Fatalf("CheckRecords = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGuardedEnginePropose(t *testing.T) {
	// rule: ARC-U-013
	tests := []struct {
		class     string
		want      error
		wantInner int
	}{
		{"ledger_book", retention.ErrNeverPurge, 0},
		{"invoice_book", retention.ErrNeverPurge, 0},
		{"long_record", nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.class, func(t *testing.T) {
			inner := &countEngine{}
			e := retention.GuardedEngine{Guard: retention.Guard{Classes: guardRegistry(t)}, Inner: inner}
			_, err := e.Propose(context.Background(), tc.class)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Propose = %v, want %v", err, tc.want)
			}
			if inner.proposed != tc.wantInner {
				t.Fatalf("inner Propose calls = %d, want %d", inner.proposed, tc.wantInner)
			}
		})
	}
}
