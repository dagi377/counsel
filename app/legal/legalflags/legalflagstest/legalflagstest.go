// Package legalflagstest builds request contexts that carry app/legal flags, for tests.
package legalflagstest

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

// Context returns a background context whose flag set defines name, turned on or off.
func Context(t testing.TB, name string, on bool) context.Context {
	t.Helper()
	return With(t, context.Background(), name, on)
}

// With returns ctx carrying a flag set that defines name, turned on or off.
func With(t testing.TB, ctx context.Context, name string, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: name, Owner: "test", Expiry: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("define flag %s: %v", name, err)
	}
	if err := s.Set(name, on); err != nil {
		t.Fatalf("set flag %s: %v", name, err)
	}
	return flags.WithSet(ctx, s)
}
