package flagsfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

func TestFakeFlags(t *testing.T) {
	tests := []struct {
		name   string
		define []string
		set    string
		on     bool
		ask    string
		want   bool
		err    error
	}{
		{"off by default", []string{"a.b"}, "", false, "a.b", false, nil},
		{"turned on", []string{"a.b"}, "a.b", true, "a.b", true, nil},
		{"undefined set", nil, "a.b", true, "a.b", false, flags.ErrUnknown},
		{"duplicate define", []string{"a.b", "a.b"}, "", false, "a.b", false, flags.ErrDuplicate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			var err error
			for _, n := range tt.define {
				if e := f.Define(flags.Def{Name: n, Owner: "o"}); e != nil {
					err = e
				}
			}
			if tt.set != "" {
				if e := f.Set(tt.set, tt.on); e != nil {
					err = e
				}
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if got := f.Enabled(tt.ask); got != tt.want {
				t.Errorf("Enabled(%q) = %v, want %v", tt.ask, got, tt.want)
			}
		})
	}
}

// rule: ARC-U-019
func TestARC_U_019_Validate(t *testing.T) {
	today := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	later := today.AddDate(0, 1, 0)
	tests := []struct {
		name string
		def  flags.Def
		err  error
	}{
		{"valid", flags.Def{Name: "a", Owner: "o", Expiry: later}, nil},
		{"expires today", flags.Def{Name: "a", Owner: "o", Expiry: today}, nil},
		{"no owner", flags.Def{Name: "a", Expiry: later}, flags.ErrNoOwner},
		{"no expiry", flags.Def{Name: "a", Owner: "o"}, flags.ErrNoExpiry},
		{"expired", flags.Def{Name: "a", Owner: "o", Expiry: today.AddDate(0, 0, -1)}, flags.ErrExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := New().Validate(context.Background(), []flags.Def{tt.def}, today); !errors.Is(err, tt.err) {
				t.Errorf("Validate() = %v, want %v", err, tt.err)
			}
		})
	}
}
