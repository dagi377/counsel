package metrics

import (
	"context"
	"errors"
	"testing"
	"time"
)

// agentPlatform simulates an optional dependency that is down for the whole test.
func agentPlatformDown(context.Context) error { return errors.New("agent platform unreachable") }

// rule: ARC-U-028
func TestARC_U_028_CoreChecksPassWithAgentPlatformDown(t *testing.T) {
	ok := func(context.Context) error { return nil }
	tests := []struct {
		name         string
		checks       map[string]CheckFunc
		wantDegraded bool
		wantFailed   []string
	}{
		{
			name: "core areas use only cell stores",
			checks: map[string]CheckFunc{
				"records": ok, "time": ok, "billing": ok, "funds": ok, "deadlines": ok,
			},
		},
		{
			name: "a core area that calls the agent platform degrades",
			checks: map[string]CheckFunc{
				"records": ok, "time": func(ctx context.Context) error { return agentPlatformDown(ctx) },
			},
			wantDegraded: true,
			wantFailed:   []string{"time"},
		},
		{
			name: "a hung check times out instead of stalling",
			checks: map[string]CheckFunc{
				"billing": func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
			},
			wantDegraded: true,
			wantFailed:   []string{"billing"},
		},
		{
			name:         "a panicking check fails",
			checks:       map[string]CheckFunc{"deadlines": func(context.Context) error { panic("boom") }},
			wantDegraded: true,
			wantFailed:   []string{"deadlines"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSmokeChecks(50 * time.Millisecond)
			for n, fn := range tt.checks {
				if err := s.Add(n, fn); err != nil {
					t.Fatalf("add %s: %v", n, err)
				}
			}
			st := s.Run(context.Background())
			if st.Degraded != tt.wantDegraded {
				t.Fatalf("degraded = %v, want %v", st.Degraded, tt.wantDegraded)
			}
			if len(st.Checks) != len(tt.checks) {
				t.Fatalf("got %d results, want %d", len(st.Checks), len(tt.checks))
			}
			var failed []string
			for i, r := range st.Checks {
				if i > 0 && st.Checks[i-1].Name > r.Name {
					t.Fatalf("results not in name order: %v", st.Checks)
				}
				if !r.OK {
					failed = append(failed, r.Name)
				}
			}
			if len(failed) != len(tt.wantFailed) || (len(failed) > 0 && failed[0] != tt.wantFailed[0]) {
				t.Fatalf("failed = %v, want %v", failed, tt.wantFailed)
			}
		})
	}
}

func TestSmokeChecksAdd(t *testing.T) {
	ok := func(context.Context) error { return nil }
	tests := []struct {
		name    string
		add     []string
		wantErr error
	}{
		{name: "unique names", add: []string{"a", "b"}},
		{name: "duplicate name", add: []string{"a", "a"}, wantErr: ErrDuplicateCheck},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSmokeChecks(0)
			var err error
			for _, n := range tt.add {
				if err = s.Add(n, ok); err != nil {
					break
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
