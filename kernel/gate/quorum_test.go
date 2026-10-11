package gate

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestARC_I_023_Quorum(t *testing.T) {
	// rule: ARC-I-023
	v1, v2 := []byte{1}, []byte{2}
	approve := func(u string, h []byte) vote { return vote{userID: u, decision: "approve", hash: h} }
	tests := []struct {
		name     string
		required int
		votes    []vote
		want     []string
		met      bool
	}{
		{"irreversible, no approvals", 2, nil, nil, false},
		{"irreversible, one approval stays pending", 2, []vote{approve("A", v2)}, []string{"A"}, false},
		{"second distinct human meets quorum", 2, []vote{approve("A", v2), approve("B", v2)}, []string{"A", "B"}, true},
		{"replayed final approval counts once", 2, []vote{approve("A", v2), approve("B", v2), approve("B", v2)}, []string{"A", "B"}, true},
		{"same human twice is one approval", 2, []vote{approve("A", v2), approve("A", v2)}, []string{"A"}, false},
		{"approval on the previous version never counts", 2, []vote{approve("A", v1), approve("B", v2)}, []string{"B"}, false},
		{"recommendation never counts", 2, []vote{approve("A", v2), {userID: "C", decision: decisionRecommend, hash: v2}}, []string{"A"}, false},
		{"rejection never counts", 1, []vote{{userID: "C", decision: "reject", hash: v2}}, nil, false},
		{"no user never counts", 1, []vote{approve("", v2)}, nil, false},
		{"single approver kind", 1, []vote{approve("A", v2)}, []string{"A"}, true},
		{"three approvers", 3, []vote{approve("A", v2), approve("B", v2), approve("C", v2)}, []string{"A", "B", "C"}, true},
		{"zero required is never met", 0, []vote{approve("A", v2)}, []string{"A"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := countQuorum(tc.required, v2, tc.votes)
			if q.Met != tc.met || !slices.Equal(q.Approvers, tc.want) || q.Required != tc.required {
				t.Fatalf("countQuorum = %+v, want approvers %v met %v", q, tc.want, tc.met)
			}
		})
	}
}

func TestARC_I_023_AlreadyDecided(t *testing.T) {
	// rule: ARC-I-023
	other := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"second decision by same human", fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23505", ConstraintName: uniqueDecision}), ErrAlreadyDecided},
		{"other unique violation", &pgconn.PgError{Code: "23505", ConstraintName: "something_else"}, nil},
		{"other error", other, other},
		{"nil", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AlreadyDecided("g1", tc.err)
			if tc.want == nil {
				if !errors.Is(got, tc.err) || errors.Is(got, ErrAlreadyDecided) {
					t.Fatalf("AlreadyDecided = %v, want %v unchanged", got, tc.err)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("AlreadyDecided = %v, want %v", got, tc.want)
			}
		})
	}
	if p := Problem(AlreadyDecided("g1", &pgconn.PgError{Code: "23505", ConstraintName: uniqueDecision})); p == nil || p.Status != 409 {
		t.Fatalf("Problem = %+v, want 409", p)
	}
}

func TestARC_I_022_Expired(t *testing.T) {
	// rule: ARC-I-022
	opened := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	ttl := 24 * time.Hour
	g := Gate{ID: "g1", CreatedAt: opened, ExpiresAt: opened.Add(ttl)}
	tests := []struct {
		name string
		g    Gate
		now  time.Time
		want bool
	}{
		{"just opened", g, opened, false},
		{"one second before ttl", g, opened.Add(ttl - time.Second), false},
		{"at ttl", g, opened.Add(ttl), true},
		{"after ttl", g, opened.Add(ttl + time.Hour), true},
		{"no expiry fails closed", Gate{ID: "g2"}, opened, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Expired(tc.g, tc.now); got != tc.want {
				t.Fatalf("Expired = %v, want %v", got, tc.want)
			}
		})
	}
}
