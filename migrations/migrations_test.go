package migrations

import (
	"slices"
	"strings"
	"testing"
)

func TestNamesAreNumberedAndOrdered(t *testing.T) {
	names := Names()
	if len(names) == 0 || names[0] != "0001_init.sql" {
		t.Fatalf("Names()[0] = %v, want 0001_init.sql first", names)
	}
	if !slices.IsSorted(names) {
		t.Fatalf("Names() not in file-name order: %v", names)
	}
	tests := []struct {
		name string
		want bool
	}{
		{"0001_init.sql", true},
		{"0300_gate_payload_recommend.sql", true},
		{"0400_ledger_book.sql", true},
		{"0500_outbox_subject_seq.sql", true},
		{"1000_work_item_subject.sql", true},
		{"1400_webhook_delivery_subject.sql", true},
		{"5500_trust_exception.sql", true},
		{"6000_court_filing.sql", true},
		{"6100_work_item_subject_backfill.sql", true},
		{"smoke.sql", false},
		{"README.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slices.Contains(names, tt.name); got != tt.want {
				t.Fatalf("Names() contains %s = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// The kernel subject backfill names an app subject kind, so it must sort after the kernel
// migration that adds the columns, and live in the app block.
func TestBackfillRunsAfterSubjectColumns(t *testing.T) {
	names := Names()
	add := slices.Index(names, "1000_work_item_subject.sql")
	fill := slices.Index(names, "6100_work_item_subject_backfill.sql")
	if add < 0 || fill < 0 || fill < add {
		t.Fatalf("subject columns at %d, backfill at %d: backfill must run after", add, fill)
	}
}

func TestReadReturnsModuleHeader(t *testing.T) {
	for _, name := range Names()[1:] {
		t.Run(name, func(t *testing.T) {
			sql, err := Read(name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(sql), "-- module: ") {
				t.Fatalf("%s: first line is not a module header", name)
			}
		})
	}
	if _, err := Read("9999_missing.sql"); err == nil {
		t.Fatal("Read of a missing migration succeeded")
	}
}
