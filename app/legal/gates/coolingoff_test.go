package gates

import (
	"errors"
	"testing"
	"time"
)

func TestCoolingOffFor(t *testing.T) {
	tests := []struct {
		code string
		want time.Duration
	}{
		{"trust.withdrawal", 24 * time.Hour},
		{"trust.transfer", 24 * time.Hour},
		{"trust.reconciliation_signoff", 24 * time.Hour},
		{"trust.regulatory_release", 24 * time.Hour},
		{"trust.receipt_classification", 0},
		{"trust.cash_over_limit", 0},
		{"billing.trust_fee_withdrawal", 0},
		{"billing.bill_approval", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			if got := CoolingOffFor(tc.code); got != tc.want {
				t.Fatalf("CoolingOffFor(%q) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}
}

func TestWithCoolingOffOnSpecKinds(t *testing.T) {
	kinds, err := ParseKinds(readSpecGates(t))
	if err != nil {
		t.Fatal(err)
	}
	got := WithCoolingOff(kinds)
	if len(got) != len(kinds) {
		t.Fatalf("len = %d, want %d", len(got), len(kinds))
	}
	raised := 0
	for i, k := range got {
		want := kinds[i].CoolingOff
		if d := CoolingOffFor(k.Code); d > want {
			want = d
			raised++
		}
		if k.CoolingOff != want {
			t.Errorf("%s: cooling off %v, want %v", k.Code, k.CoolingOff, want)
		}
	}
	if raised != len(coolingOffKinds) {
		t.Fatalf("raised %d kinds, want every one of %v present in spec/gates.json", raised, coolingOffKinds)
	}
}

func TestWithCoolingOffKeepsLonger(t *testing.T) {
	in := []SpecKind{
		{Kind: Kind{Code: "trust.withdrawal", CoolingOff: 48 * time.Hour}},
		{Kind: Kind{Code: "trust.transfer"}},
	}
	got := WithCoolingOff(in)
	if got[0].CoolingOff != 48*time.Hour || got[1].CoolingOff != TrustCoolingOff {
		t.Fatalf("got %v, %v", got[0].CoolingOff, got[1].CoolingOff)
	}
	if in[1].CoolingOff != 0 {
		t.Fatal("input mutated")
	}
}

func TestCheckCoolingOff(t *testing.T) {
	tor, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	opened := time.Date(2026, 10, 7, 10, 0, 0, 0, tor)
	trust := Kind{Code: "trust.withdrawal", CoolingOff: TrustCoolingOff}
	bill := Kind{Code: "billing.bill_approval"}
	g := Gate{ID: "g1", OpenedAt: opened}
	tests := []struct {
		name string
		kind Kind
		now  time.Time
		err  error
	}{
		{"trust at open", trust, opened, ErrCoolingOff},
		{"trust one second short", trust, opened.Add(TrustCoolingOff - time.Second), ErrCoolingOff},
		{"trust exactly 24h", trust, opened.Add(TrustCoolingOff), nil},
		{"trust after", trust, opened.Add(25 * time.Hour), nil},
		{"no cooling off", bill, opened, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckCoolingOff(tc.kind, g, tc.now); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}
