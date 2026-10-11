package legalflags

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

func TestEnabledReadsContextSet(t *testing.T) {
	s := flags.New()
	if err := s.Define(flags.Def{Name: TrustBylaw9Verified, Owner: "lawyer/LSO", Expiry: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		ctx  func() context.Context
		flag string
		want bool
	}{
		{"no set in ctx", context.Background, TrustBylaw9Verified, false},
		{"defined, off", func() context.Context { return flags.WithSet(context.Background(), s) }, TrustBylaw9Verified, false},
		{"undefined", func() context.Context { return flags.WithSet(context.Background(), s) }, TaxVerifyRules, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Enabled(tt.ctx(), tt.flag); got != tt.want {
				t.Fatalf("Enabled = %v, want %v", got, tt.want)
			}
		})
	}
	if err := s.Set(TrustBylaw9Verified, true); err != nil {
		t.Fatal(err)
	}
	if !Enabled(flags.WithSet(context.Background(), s), TrustBylaw9Verified) {
		t.Fatal("Enabled = false after Set(true)")
	}
}
