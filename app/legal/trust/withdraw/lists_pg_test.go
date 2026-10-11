package withdraw

import "testing"

func TestContractStatus(t *testing.T) {
	tests := []struct {
		db   string
		want Status
	}{
		{"proposed", StatusRequested},
		{"pending_authorization", StatusRequested},
		{"authorized", StatusAuthorized},
		{"executed", StatusExecuted},
		{"applied", StatusExecuted},
		{"confirmed", StatusExecuted},
		{"rejected", StatusRejected},
		{"cancelled", StatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.db, func(t *testing.T) {
			if got := contractStatus(tt.db); got != tt.want {
				t.Fatalf("contractStatus(%q) = %q, want %q", tt.db, got, tt.want)
			}
		})
	}
}

func TestStatusVersionMovesForward(t *testing.T) {
	paths := [][]string{
		{"proposed", "pending_authorization", "authorized", "executed", "applied", "confirmed"},
		{"pending_authorization", "rejected"},
		{"pending_authorization", "cancelled"},
		{"authorized", "cancelled"},
	}
	for _, p := range paths {
		for i := 1; i < len(p); i++ {
			if statusVersion[p[i]] <= statusVersion[p[i-1]] {
				t.Errorf("%s -> %s does not raise the version", p[i-1], p[i])
			}
		}
	}
}

func TestDBStatuses(t *testing.T) {
	tests := []struct {
		s    Status
		want int
	}{
		{StatusRequested, 2},
		{StatusExecuted, 3},
		{StatusCancelled, 1},
		{Status("unknown"), 0},
	}
	for _, tt := range tests {
		t.Run(string(tt.s), func(t *testing.T) {
			if got := len(dbStatuses(tt.s)); got != tt.want {
				t.Fatalf("len(dbStatuses(%q)) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}
