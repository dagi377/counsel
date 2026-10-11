package reportsfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/reports"
)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 12, 0, 0, 0, time.UTC) }

func TestHST(t *testing.T) {
	f := New()
	f.AddTax(day(1, 5), 10400, false)
	f.AddTax(day(2, 10), 1300, false)
	f.AddTax(day(2, 20), 260, true)
	f.AddTax(day(4, 1), 999, false)
	tests := []struct {
		name    string
		p       reports.Period
		want    int64
		wantErr error
	}{
		{name: "quarter", p: reports.Period{From: day(1, 1), To: day(3, 31)}, want: 10400 + 1300 - 260},
		{name: "february", p: reports.Period{From: day(2, 1), To: day(2, 28)}, want: 1300 - 260},
		{name: "inverted", p: reports.Period{From: day(3, 1), To: day(2, 1)}, wantErr: reports.ErrInvalidPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.HST(context.Background(), tt.p)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HST err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (got.NetTaxCents != tt.want || got.NetTaxCents != got.BillTaxCents-got.CreditTaxCents) {
				t.Fatalf("HST = %+v, want net %d", got, tt.want)
			}
		})
	}
}

func TestTrustListing(t *testing.T) {
	f := New()
	f.SetTrust("ta1", 30000, reports.TrustLine{ClientID: "c1", MatterID: "m1", BalanceCents: 10000}, reports.TrustLine{ClientID: "c2", MatterID: "m2", BalanceCents: 20000})
	f.SetTrust("ta2", 5000, reports.TrustLine{ClientID: "c1", MatterID: "m3", BalanceCents: 4000})
	tests := []struct {
		account string
		want    error
	}{
		{account: "ta1"},
		{account: "ta2", want: reports.ErrOutOfBalance},
		{account: "zz", want: reports.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.account, func(t *testing.T) {
			l, err := f.TrustListing(context.Background(), tt.account, day(1, 31))
			if !errors.Is(err, tt.want) {
				t.Fatalf("TrustListing err = %v, want %v", err, tt.want)
			}
			if err == nil && l.TotalCents != l.LedgerCents {
				t.Fatalf("total %d != ledger %d", l.TotalCents, l.LedgerCents)
			}
		})
	}
}

func TestAging(t *testing.T) {
	asOf := day(6, 30)
	tests := []struct {
		name    string
		billed  time.Time
		bucket  reports.AgingBucket
		ledger  int64
		wantErr error
	}{
		{name: "30 days", billed: asOf.AddDate(0, 0, -30), bucket: reports.Days0To30, ledger: 100},
		{name: "31 days", billed: asOf.AddDate(0, 0, -31), bucket: reports.Days31To60, ledger: 100},
		{name: "90 days", billed: asOf.AddDate(0, 0, -90), bucket: reports.Days61To90, ledger: 100},
		{name: "91 days", billed: asOf.AddDate(0, 0, -91), bucket: reports.DaysOver90, ledger: 100},
		{name: "off ledger", billed: asOf, bucket: reports.Days0To30, ledger: 99, wantErr: reports.ErrOutOfBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			f.AddOpenBill(tt.billed, 100)
			f.SetARLedger(tt.ledger)
			a, err := f.Aging(context.Background(), asOf)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Aging err = %v, want %v", err, tt.wantErr)
			}
			if a.Buckets[tt.bucket] != 100 || a.TotalCents != 100 {
				t.Fatalf("Aging = %+v, want 100 in bucket %d", a, tt.bucket)
			}
		})
	}
}

func TestWIP(t *testing.T) {
	f := New()
	f.AddWIP("m2", day(1, 2), 5000)
	f.AddWIP("m1", day(1, 3), 2500)
	f.AddWIP("m1", day(1, 4), 2500)
	f.AddWIP("m1", day(2, 1), 9999)
	tests := []struct {
		name  string
		asOf  time.Time
		total int64
		lines int
	}{
		{name: "january", asOf: day(1, 31), total: 10000, lines: 2},
		{name: "before any", asOf: day(1, 1), total: 0, lines: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := f.WIP(context.Background(), tt.asOf)
			if err != nil || w.TotalCents != tt.total || len(w.Lines) != tt.lines {
				t.Fatalf("WIP = %+v, %v", w, err)
			}
			if tt.lines > 0 && w.Lines[0].MatterID != "m1" {
				t.Fatalf("lines not ordered: %+v", w.Lines)
			}
		})
	}
}
