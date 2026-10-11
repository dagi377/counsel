package trust

import "testing"

func TestListLimit(t *testing.T) {
	tests := []struct{ in, want int }{
		{-1, DefaultListLimit},
		{0, DefaultListLimit},
		{1, 1},
		{MaxListLimit, MaxListLimit},
		{MaxListLimit + 1, MaxListLimit},
	}
	for _, tt := range tests {
		if got := ListLimit(tt.in); got != tt.want {
			t.Errorf("ListLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
