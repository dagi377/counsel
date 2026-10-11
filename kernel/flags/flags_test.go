package flags

import (
	"context"
	"errors"
	"testing"
)

func TestEnabled(t *testing.T) {
	tests := []struct {
		name   string
		define []string
		turnOn []string
		ask    string
		noSet  bool
		want   bool
	}{
		{name: "undefined is off", ask: "x.y", want: false},
		{name: "defined is off by default", define: []string{"x.y"}, ask: "x.y", want: false},
		{name: "defined and on", define: []string{"x.y"}, turnOn: []string{"x.y"}, ask: "x.y", want: true},
		{name: "other flag on", define: []string{"x.y", "x.z"}, turnOn: []string{"x.z"}, ask: "x.y", want: false},
		{name: "no set in context", noSet: true, ask: "x.y", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			for _, n := range tt.define {
				if err := s.Define(Def{Name: n, Owner: "test"}); err != nil {
					t.Fatalf("define: %v", err)
				}
			}
			for _, n := range tt.turnOn {
				if err := s.Set(n, true); err != nil {
					t.Fatalf("set: %v", err)
				}
			}
			ctx := context.Background()
			if !tt.noSet {
				ctx = WithSet(ctx, s)
			}
			if got := Enabled(ctx, tt.ask); got != tt.want {
				t.Fatalf("Enabled(%q) = %v, want %v", tt.ask, got, tt.want)
			}
		})
	}
}

func TestDefineAndSetErrors(t *testing.T) {
	tests := []struct {
		name string
		run  func(s *Set) error
		want error
	}{
		{name: "duplicate", run: func(s *Set) error {
			if err := s.Define(Def{Name: "a.b"}); err != nil {
				return err
			}
			return s.Define(Def{Name: "a.b"})
		}, want: ErrDuplicate},
		{name: "empty name", run: func(s *Set) error { return s.Define(Def{}) }, want: ErrUnknown},
		{name: "set unknown", run: func(s *Set) error { return s.Set("nope", true) }, want: ErrUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(New()); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
