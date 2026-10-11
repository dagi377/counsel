package draft

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/tax"
)

// places is a scripted Places: the supply facts per matter.
type places map[string]tax.Supply

func (p places) Supply(_ context.Context, _ pgx.Tx, matterID string) (tax.Supply, error) {
	s, ok := p[matterID]
	if !ok {
		return tax.Supply{}, errNoMatter
	}
	return s, nil
}

var errNoMatter = errors.New("no such matter")

func taxedDrafts(p places) PGDrafts {
	return PGDrafts{Places: p, Decider: tax.NewPlaceOfSupply(), Calc: tax.NewCalculator(tax.NewRateTable(tax.Seed())),
		Now: func() time.Time { return time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC) }}
}

// rule: BIL-C-021
func TestBIL_C_021_PGDraftsTotals(t *testing.T) {
	d := taxedDrafts(places{"m": {ClientProvince: "ON", RecipientResident: true}})
	sup, err := d.place(context.Background(), nil, "m")
	if err != nil {
		t.Fatal(err)
	}
	lines := []pgLine{
		{source: bill.SourceTime, amount: 100000, taxCode: "HST-ON"},
		{source: bill.SourceExpense, amount: 20000, taxCode: "HST-ON"},
		{source: bill.SourceExpense, amount: 7000, taxCode: bill.TaxCodeExempt},
	}
	tests := []struct {
		name               string
		discount           int64
		want               error
		tax, total, nontax int64
	}{
		{"no discount", 0, nil, 15600, 142600, 7000},
		{"discount lowers the tax base", 10000, nil, 14300, 131300, 7000},
		{"discount above fees and own costs", 120001, bill.ErrInvalid, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.compute(context.Background(), lines, tt.discount, sup)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			if got.tax != tt.tax || got.total != tt.total || got.nontaxable != tt.nontax ||
				got.total != got.fees+got.taxable+got.nontaxable-got.discount+got.tax {
				t.Fatalf("totals = %+v", got)
			}
		})
	}
}

// rule: BIL-T-010
// Draft tax follows the place of supply from the matter and client data: HST in a
// participating province, GST elsewhere, and an unknown place refuses the draft (fail closed).
func TestBIL_T_010_DraftTaxByPlaceOfSupply(t *testing.T) {
	d := taxedDrafts(places{
		"on":      {ClientProvince: "ON", RecipientResident: true},
		"ab":      {ClientProvince: "AB", RecipientResident: true},
		"mb":      {ClientProvince: "MB", RecipientResident: true},
		"court":   {ClientProvince: "AB", CourtProvince: "NS", RecipientResident: true},
		"unknown": {},
		"bad":     {ClientProvince: "XX", CourtProvince: "XX"},
	})
	lines := []pgLine{
		{source: bill.SourceTime, amount: 100000},
		{source: bill.SourceExpense, amount: 7000, taxCode: bill.TaxCodeExempt},
	}
	tests := []struct {
		name, matter   string
		want           error
		province, code string
		breakdown      []bill.TaxLine
	}{
		{"ON client pays ON HST", "on", nil, "ON", "HST-ON",
			[]bill.TaxLine{{TaxCode: "HST-ON", Component: "HST", RatePPM: 130000, BaseCents: 100000, TaxCents: 13000}}},
		{"AB client, non-participating, pays GST only", "ab", nil, "AB", "GST",
			[]bill.TaxLine{{TaxCode: "GST", Component: "GST", RatePPM: 50000, BaseCents: 100000, TaxCents: 5000}}},
		{"MB client, firm not registered for RST, pays GST only", "mb", nil, "MB", "GST",
			[]bill.TaxLine{{TaxCode: "GST", Component: "GST", RatePPM: 50000, BaseCents: 100000, TaxCents: 5000}}},
		{"NS litigation beats the AB address", "court", nil, "NS", "HST-NS",
			[]bill.TaxLine{{TaxCode: "HST-NS", Component: "HST", RatePPM: 140000, BaseCents: 100000, TaxCents: 14000}}},
		{"no address and not resident is unknown", "unknown", ErrPlaceUnknown, "", "", nil},
		{"unknown court province is unknown", "bad", ErrPlaceUnknown, "", "", nil},
		{"matter without supply facts refused", "missing", errNoMatter, "", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sup, err := d.place(context.Background(), nil, tt.matter)
			if !errors.Is(err, tt.want) {
				t.Fatalf("place err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			if sup.province() != tt.province || sup.lineCode() != tt.code {
				t.Fatalf("place = %s %s, want %s %s", sup.province(), sup.lineCode(), tt.province, tt.code)
			}
			got, err := d.compute(context.Background(), lines, 0, sup)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.breakdown, tt.breakdown) || got.total != 107000+tt.breakdown[0].TaxCents {
				t.Fatalf("breakdown = %+v total %d, want %+v", got.breakdown, got.total, tt.breakdown)
			}
		})
	}
}

// A draft without the place-of-supply engine is refused rather than taxed at a default.
func TestPGDraftsNeedTaxEngine(t *testing.T) {
	full := taxedDrafts(places{})
	full.WIP, full.Payers = noWIP{}, noPayers{}
	tests := []struct {
		name string
		d    func() PGDrafts
		want error
	}{
		{"complete", func() PGDrafts { return full }, nil},
		{"no places", func() PGDrafts { d := full; d.Places = nil; return d }, bill.ErrInvalid},
		{"no decider", func() PGDrafts { d := full; d.Decider = nil; return d }, bill.ErrInvalid},
		{"no calculator", func() PGDrafts { d := full; d.Calc = nil; return d }, bill.ErrInvalid},
		{"no clock", func() PGDrafts { d := full; d.Now = nil; return d }, bill.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.d().ready(); !errors.Is(err, tt.want) {
				t.Fatalf("ready = %v, want %v", err, tt.want)
			}
		})
	}
}

type noWIP struct{}

func (noWIP) Unbilled(context.Context, pgx.Tx, string, time.Time, time.Time) ([]WIPItem, error) {
	return nil, nil
}
func (noWIP) Claim(context.Context, pgx.Tx, string, []WIPItem) error { return nil }

type noPayers struct{}

func (noPayers) Payer(context.Context, pgx.Tx, string) (string, error) { return "", nil }

func TestBIL_C_023_LineActions(t *testing.T) {
	tests := []struct {
		name   string
		a      bill.LineAction
		amount int64
		keep   bool
		want   error
	}{
		{"bill", bill.LineAction{Action: "bill"}, 5000, true, nil},
		{"hold stays unbilled", bill.LineAction{Action: "hold"}, 0, false, nil},
		{"no charge bills zero", bill.LineAction{Action: "no_charge"}, 0, true, nil},
		{"write-down", bill.LineAction{Action: "write_down", WriteDownCents: 1000, ReasonCode: "courtesy"}, 4000, true, nil},
		{"write-down needs a reason", bill.LineAction{Action: "write_down", WriteDownCents: 1000}, 0, false, ErrReasonRequired},
		{"write-down below zero", bill.LineAction{Action: "write_down", WriteDownCents: 6000, ReasonCode: "courtesy"}, 0, false, ErrNegativeLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, keep, err := applyAction(5000, tt.a)
			if !errors.Is(err, tt.want) || amount != tt.amount || keep != tt.keep {
				t.Fatalf("applyAction = %d %v %v, want %d %v %v", amount, keep, err, tt.amount, tt.keep, tt.want)
			}
		})
	}
}

// rule: BIL-T-015
// A zero-rated non-resident draft stores the export place and one zero-rate line; its
// province column is the ExportProvince marker, never NULL, so approval accepts it.
func TestBIL_T_015_ZeroRatedDraftIsExport(t *testing.T) {
	d := taxedDrafts(places{
		"uk-corp": {ClientProvince: "GB", RecipientBusiness: true},
		"on":      {ClientProvince: "ON", RecipientResident: true},
	})
	tests := []struct {
		name, matter, province, jurisdiction, code string
		breakdown                                  []bill.TaxLine
		tax                                        int64
	}{
		{"non-resident corporation is an export", "uk-corp", ExportProvince, PlaceExport, tax.Zero,
			[]bill.TaxLine{{TaxCode: tax.Zero, Component: tax.Zero, RatePPM: 0, BaseCents: 100000, TaxCents: 0}}, 0},
		{"resident client is not an export", "on", "ON", "ON", "HST-ON",
			[]bill.TaxLine{{TaxCode: "HST-ON", Component: "HST", RatePPM: 130000, BaseCents: 100000, TaxCents: 13000}}, 13000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sup, err := d.place(context.Background(), nil, tt.matter)
			if err != nil {
				t.Fatal(err)
			}
			if sup.province() == "" || len(sup.province()) != 2 {
				t.Fatalf("province %q: an approved bill needs a two-letter place_of_supply_province", sup.province())
			}
			detail, err := sup.detail()
			if err != nil {
				t.Fatal(err)
			}
			if sup.province() != tt.province || sup.lineCode() != tt.code || !strings.Contains(detail, `"jurisdiction":"`+tt.jurisdiction+`"`) {
				t.Fatalf("place = %s %s %s", sup.province(), sup.lineCode(), detail)
			}
			got, err := d.compute(context.Background(), []pgLine{{source: bill.SourceTime, amount: 100000}}, 0, sup)
			if err != nil {
				t.Fatal(err)
			}
			if got.tax != tt.tax || !slices.Equal(got.breakdown, tt.breakdown) {
				t.Fatalf("tax %d breakdown %+v, want %d %+v", got.tax, got.breakdown, tt.tax, tt.breakdown)
			}
		})
	}
}
