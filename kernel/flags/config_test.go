package flags

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConfigValidator(t *testing.T) {
	tor, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 10, 11, 9, 0, 0, 0, tor)
	good := Def{Name: "billing.x", Owner: "ops", Expiry: today.AddDate(0, 1, 0)}
	// rule: ARC-U-019
	tests := []struct {
		name string
		defs []Def
		want []error
	}{
		{"valid", []Def{good}, nil},
		{"empty", nil, nil},
		{"expires today", []Def{{Name: "a", Owner: "ops", Expiry: time.Date(2026, 10, 11, 0, 0, 0, 0, tor)}}, nil},
		{"no owner", []Def{{Name: "a", Expiry: good.Expiry}}, []error{ErrNoOwner}},
		{"no expiry", []Def{{Name: "a", Owner: "ops"}}, []error{ErrNoExpiry}},
		{"expired", []Def{{Name: "a", Owner: "ops", Expiry: today.AddDate(0, 0, -1)}}, []error{ErrExpired}},
		{"all bad listed", []Def{
			{Name: "a", Expiry: good.Expiry},
			{Name: "b", Owner: "ops"},
			good,
		}, []error{ErrNoOwner, ErrNoExpiry}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ConfigValidator{}.Validate(context.Background(), tc.defs, today)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			for _, w := range tc.want {
				if !errors.Is(err, w) {
					t.Errorf("Validate = %v, want %v", err, w)
				}
			}
		})
	}
}

func TestLoad(t *testing.T) {
	today := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	exp := today.AddDate(0, 0, 30)
	// rule: ARC-U-019
	tests := []struct {
		name    string
		defs    []Def
		wantErr error
		wantN   int
	}{
		{"loads all off", []Def{{Name: "a", Owner: "o", Expiry: exp}, {Name: "b", Owner: "o", Expiry: exp}}, nil, 2},
		{"no owner fails load", []Def{{Name: "a", Expiry: exp}}, ErrNoOwner, 0},
		{"no expiry fails load", []Def{{Name: "a", Owner: "o"}}, ErrNoExpiry, 0},
		{"duplicate fails load", []Def{{Name: "a", Owner: "o", Expiry: exp}, {Name: "a", Owner: "o", Expiry: exp}}, ErrDuplicate, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Load(context.Background(), tc.defs, today)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || s != nil {
					t.Fatalf("Load = %v, %v; want nil, %v", s, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := len(s.Defs()); got != tc.wantN {
				t.Fatalf("defs = %d, want %d", got, tc.wantN)
			}
			for _, d := range s.Defs() {
				if s.Enabled(d.Name) {
					t.Errorf("flag %q on after load, want off", d.Name)
				}
			}
		})
	}
}

func TestConfigValidatorCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (ConfigValidator{}).Validate(ctx, nil, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate = %v, want context.Canceled", err)
	}
}
