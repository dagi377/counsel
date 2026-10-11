package migrations

import (
	"slices"
	"testing"
)

func TestContractMark(t *testing.T) {
	tests := []struct {
		name, sql string
		want      bool
	}{
		{"marked", "-- module: kernel/gate\n-- contract: 2026.11\nALTER TABLE x DROP COLUMN y;\n", true},
		{"marked with spaces", "--   Contract:   r7  \n", true},
		{"no release", "-- contract:\n", false},
		{"mark inside a sentence", "-- this is not a contract: file\n", false},
		{"expand file", "-- module: kernel/gate\nALTER TABLE x ADD COLUMN y int;\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contractMark.MatchString(tt.sql); got != tt.want {
				t.Fatalf("contractMark(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

// Every embedded file falls in exactly one of the two lists, in apply order.
func TestSplitCoversEveryFile(t *testing.T) {
	expand, contract, err := Split()
	if err != nil {
		t.Fatal(err)
	}
	all := append(slices.Clone(expand), contract...)
	slices.Sort(all)
	if !slices.Equal(all, Names()) {
		t.Fatalf("expand+contract = %v, want %v", all, Names())
	}
	if !slices.IsSorted(expand) || !slices.IsSorted(contract) {
		t.Fatalf("lists not in apply order: %v / %v", expand, contract)
	}
	for _, name := range contract {
		if c, err := IsContract(name); err != nil || !c {
			t.Fatalf("IsContract(%s) = %v, %v", name, c, err)
		}
	}
	if len(expand) == 0 || expand[0] != "0001_init.sql" {
		t.Fatalf("expand = %v, want 0001_init.sql first", expand)
	}
}
