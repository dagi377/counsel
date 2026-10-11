package books

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestValidateVendorBill(t *testing.T) {
	day := time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC)
	ok := VendorBill{Vendor: "Courier", BillDate: day, AmountCents: 1000}
	tests := []struct {
		name   string
		mutate func(*VendorBill)
		want   error
	}{
		{"valid", func(*VendorBill) {}, nil},
		{"with tax", func(b *VendorBill) { b.TaxCents = 130 }, nil},
		{"no vendor", func(b *VendorBill) { b.Vendor = "  " }, ErrInvalid},
		{"no bill date", func(b *VendorBill) { b.BillDate = time.Time{} }, ErrInvalid},
		{"zero amount", func(b *VendorBill) { b.AmountCents = 0 }, ErrInvalid},
		{"negative amount", func(b *VendorBill) { b.AmountCents = -1 }, ErrInvalid},
		{"negative tax", func(b *VendorBill) { b.TaxCents = -1 }, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := ok
			tt.mutate(&b)
			if err := validateVendorBill(b); !errors.Is(err, tt.want) {
				t.Fatalf("validate = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestVendorBillStatusMoves(t *testing.T) {
	tests := []struct {
		from, want VendorBillStatus
		err        error
	}{
		{VendorBillOpen, VendorBillPaid, nil},
		{VendorBillOpen, VendorBillVoid, nil},
		{VendorBillOpen, VendorBillOpen, ErrBadStatus},
		{VendorBillPaid, VendorBillVoid, ErrBadStatus},
		{VendorBillPaid, VendorBillPaid, ErrBadStatus},
		{VendorBillVoid, VendorBillPaid, ErrBadStatus},
		{VendorBillVoid, VendorBillVoid, ErrBadStatus},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.want), func(t *testing.T) {
			got, err := nextStatus(tt.from, tt.want)
			if !errors.Is(err, tt.err) || (err == nil && got != tt.want) {
				t.Fatalf("nextStatus = %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestCivilDateUsesBusinessZone(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skip("no tzdata")
	}
	tests := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, time.April, 1, 2, 0, 0, 0, time.UTC), "2026-03-31"},
		{time.Date(2026, time.April, 1, 5, 0, 0, 0, time.UTC), "2026-04-01"},
	}
	for _, tt := range tests {
		if got := civil(tt.at, loc); got != tt.want {
			t.Errorf("civil(%v) = %s, want %s", tt.at, got, tt.want)
		}
	}
}

// gl.vendor_bill is owned by app/legal/books: the data model puts it in the app layer of the
// gl schema, and the store's statements touch only that table and never delete.
func TestVendorBillOwnedByBooks(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "spec", "eng", "a4-data-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dm struct {
		Tables []struct {
			Name   string `json:"name"`
			Layer  string `json:"layer"`
			Module string `json:"module"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(raw, &dm); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tb := range dm.Tables {
		if tb.Name == "gl.vendor_bill" {
			found = true
			if tb.Layer != "app" || tb.Module != "gl" {
				t.Fatalf("gl.vendor_bill layer %q module %q, want app in gl", tb.Layer, tb.Module)
			}
		}
	}
	if !found {
		t.Fatal("gl.vendor_bill missing from data model")
	}
	table := regexp.MustCompile(`(?i)\b(?:FROM|INTO|UPDATE|JOIN)\s+([a-z_]+\.[a-z_]+)`)
	forbidden := regexp.MustCompile(`(?i)\b(DELETE|TRUNCATE|JOIN)\b`)
	for i, s := range vendorBillStatements {
		if forbidden.MatchString(s) {
			t.Errorf("statement %d deletes or joins: %s", i, s)
		}
		ms := table.FindAllStringSubmatch(s, -1)
		if len(ms) == 0 {
			t.Errorf("statement %d names no table: %s", i, s)
		}
		for _, m := range ms {
			if m[1] != "gl.vendor_bill" {
				t.Errorf("statement %d touches %s, want only gl.vendor_bill", i, m[1])
			}
		}
	}
}
