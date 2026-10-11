package retention_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/retention"
)

func TestRegisterClass(t *testing.T) {
	tests := []struct {
		name  string
		pre   []retention.Class
		class retention.Class
		want  error
	}{
		{"valid", nil, retention.Class{ID: "long_record", MinYears: 6, DestructionGate: "record_destroy"}, nil},
		{"never purge needs no gate", nil, retention.Class{ID: "permanent_book", NeverPurge: true}, nil},
		{"empty id", nil, retention.Class{MinYears: 1, DestructionGate: "g"}, retention.ErrInvalidClass},
		{"negative years", nil, retention.Class{ID: "x", MinYears: -1, DestructionGate: "g"}, retention.ErrInvalidClass},
		{"purgeable without gate", nil, retention.Class{ID: "x", MinYears: 1}, retention.ErrInvalidClass},
		{"duplicate", []retention.Class{{ID: "x", MinYears: 1, DestructionGate: "g"}}, retention.Class{ID: "x", MinYears: 2, DestructionGate: "g"}, retention.ErrDuplicateClass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := retention.NewRegistry()
			for _, c := range tt.pre {
				if err := g.RegisterClass(c); err != nil {
					t.Fatalf("pre-register: %v", err)
				}
			}
			err := g.RegisterClass(tt.class)
			if !errors.Is(err, tt.want) {
				t.Fatalf("RegisterClass() = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				return
			}
			got, err := g.Class(tt.class.ID)
			if err != nil || got != tt.class {
				t.Fatalf("Class() = %+v, %v; want %+v", got, err, tt.class)
			}
		})
	}
}

func TestClassUnknown(t *testing.T) {
	g := retention.NewRegistry()
	if _, err := g.Class("nope"); !errors.Is(err, retention.ErrUnknownClass) {
		t.Fatalf("Class() = %v, want ErrUnknownClass", err)
	}
}

func TestClassDue(t *testing.T) {
	tor, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, tor) }
	file := retention.Class{ID: "long_record", MinYears: 6, DestructionGate: "record_destroy"}
	ledger := retention.Class{ID: "permanent_book", NeverPurge: true}
	asOf := day(2026, time.October, 11)
	tests := []struct {
		name  string
		class retention.Class
		rec   retention.Record
		want  bool
	}{
		{"past minimum", file, retention.Record{ID: "r1", Class: "long_record", ClosedOn: day(2019, time.January, 1)}, true},
		{"exactly at minimum", file, retention.Record{ID: "r2", Class: "long_record", ClosedOn: day(2020, time.October, 11)}, true},
		{"one day short", file, retention.Record{ID: "r3", Class: "long_record", ClosedOn: day(2020, time.October, 12)}, false},
		{"hold blocks", file, retention.Record{ID: "r4", Class: "long_record", ClosedOn: day(2010, time.January, 1), Hold: true}, false},
		{"not closed", file, retention.Record{ID: "r5", Class: "long_record"}, false},
		{"other class", file, retention.Record{ID: "r6", Class: "other", ClosedOn: day(2010, time.January, 1)}, false},
		{"never purge", ledger, retention.Record{ID: "r7", Class: "permanent_book", ClosedOn: day(1990, time.January, 1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.class.Due(tt.rec, asOf); got != tt.want {
				t.Fatalf("Due() = %v, want %v", got, tt.want)
			}
		})
	}
}
