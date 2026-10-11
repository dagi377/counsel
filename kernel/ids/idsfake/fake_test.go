package idsfake

import (
	"testing"

	"github.com/dagi377/counsel/kernel/ids"
)

func TestFakeNew(t *testing.T) {
	tests := []struct {
		name  string
		start uint64
		n     int
		first ids.ID
		last  ids.ID
	}{
		{"from zero", 0, 3, "00000000-0000-7000-8000-000000000000", "00000000-0000-7000-8000-000000000002"},
		{"from 255", 255, 2, "00000000-0000-7000-8000-0000000000ff", "00000000-0000-7000-8000-000000000100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(tt.start)
			var prev ids.ID
			for i := 0; i < tt.n; i++ {
				id := f.New()
				if len(id) != 36 {
					t.Fatalf("id %q is not canonical length", id)
				}
				if i > 0 && id <= prev {
					t.Fatalf("id %q not greater than %q", id, prev)
				}
				prev = id
			}
			got := f.Issued()
			if got[0] != tt.first || got[len(got)-1] != tt.last || len(got) != tt.n {
				t.Fatalf("Issued() = %v", got)
			}
		})
	}
}
