package metricsfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/metrics"
)

func TestFakeRecorder(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		labels map[string]string
		err    error
	}{
		{"no labels", nil, nil},
		{"allowed", map[string]string{"route": "/x", "status": "200"}, nil},
		{"customer label", map[string]string{"client": "someone"}, metrics.ErrLabel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if err := f.Inc(ctx, "req", tt.labels); !errors.Is(err, tt.err) {
				t.Fatalf("Inc() = %v, want %v", err, tt.err)
			}
			if err := f.Observe(ctx, "lat", time.Millisecond, tt.labels); !errors.Is(err, tt.err) {
				t.Fatalf("Observe() = %v, want %v", err, tt.err)
			}
			want := 0
			if tt.err == nil {
				want = 1
			}
			if f.Count("req") != want || len(f.Samples("lat")) != want {
				t.Errorf("count %d samples %d, want %d", f.Count("req"), len(f.Samples("lat")), want)
			}
		})
	}
}

func TestFakeChecks(t *testing.T) {
	pass := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("db row for someone") }
	tests := []struct {
		name     string
		checks   map[string]metrics.CheckFunc
		degraded bool
	}{
		{"none", nil, false},
		{"all pass", map[string]metrics.CheckFunc{"a": pass, "b": pass}, false},
		{"one fails", map[string]metrics.CheckFunc{"a": pass, "b": fail}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			for n, fn := range tt.checks {
				if err := f.Add(n, fn); err != nil {
					t.Fatal(err)
				}
			}
			st := f.Run(context.Background())
			if st.Degraded != tt.degraded || len(st.Checks) != len(tt.checks) {
				t.Errorf("Run() = %+v", st)
			}
			for i := 1; i < len(st.Checks); i++ {
				if st.Checks[i-1].Name > st.Checks[i].Name {
					t.Errorf("checks not sorted: %+v", st.Checks)
				}
			}
		})
	}
	f := New()
	_ = f.Add("a", pass)
	if err := f.Add("a", pass); !errors.Is(err, metrics.ErrDuplicateCheck) {
		t.Errorf("duplicate Add() = %v", err)
	}
}
