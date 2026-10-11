package tax

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
)

// tableRow is one decision_table row of spec/eng/b2-sales-tax.json.
type tableRow struct {
	ClientLocation string          `json:"client_location"`
	ServiceType    string          `json:"service_type"`
	PlaceOfSupply  string          `json:"place_of_supply"`
	Taxes          json.RawMessage `json:"taxes"`
	Rules          []string        `json:"rules"`
	Verify         bool            `json:"verify"`
}

type tableTax struct {
	Kind         string    `json:"kind"`
	Jurisdiction string    `json:"jurisdiction"`
	RatePPM      money.PPM `json:"rate_ppm"`
	Condition    string    `json:"condition"`
}

const (
	tableCopy = "testdata/b2-decision-table.json"
	tableSpec = "../../../../spec/eng/b2-sales-tax.json"
	tableBase = money.Cents(100000)
)

func loadTable(t *testing.T) []tableRow {
	t.Helper()
	b, err := os.ReadFile(tableCopy)
	if err != nil {
		t.Fatal(err)
	}
	var rows []tableRow
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// The testdata copy must stay identical to the spec's decision_table.
func TestDecisionTableCopyIdentical(t *testing.T) {
	var spec struct {
		DecisionTable any `json:"decision_table"`
	}
	var copied any
	for path, dst := range map[string]any{tableSpec: &spec, tableCopy: &copied} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, dst); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if !reflect.DeepEqual(spec.DecisionTable, copied) {
		t.Fatalf("%s differs from decision_table in %s", tableCopy, tableSpec)
	}
	if n := len(loadTable(t)); n != 49 {
		t.Fatalf("decision table has %d rows, want 49", n)
	}
}

// supplyVariant is one concrete supply a table row stands for.
type supplyVariant struct {
	name string
	s    Supply
}

// variants turns a row into the supplies it describes. A row naming a class of clients
// ("any resident or non-resident") is run once per member. ok is false for rows that are not
// run here: the federal-court row (TestDecisionTableFederalCourt) and the disbursement,
// interest and surcharge rows (TestDecisionTableOutsideDecide).
func variants(t *testing.T, r tableRow) ([]supplyVariant, bool) {
	t.Helper()
	reg := func(s Supply) Supply {
		// The (verify) provincial rows are conditional on the firm's registration.
		if r.Verify {
			s.RegisteredProvinces = []string{r.PlaceOfSupply}
		}
		s.Date = posDate
		return s
	}
	svc, loc := r.ServiceType, r.ClientLocation
	switch {
	case strings.HasPrefix(svc, "general (advisory") && provinces[loc]:
		return []supplyVariant{{"resident", reg(Supply{ClientProvince: loc, RecipientResident: true})}}, true
	case strings.HasPrefix(svc, "general (advisory") && strings.HasPrefix(loc, "non-resident"):
		return []supplyVariant{
			{"individual", reg(Supply{ClientProvince: "US"})},
			{"business", reg(Supply{ClientProvince: "US", RecipientBusiness: true})},
		}, true
	case strings.HasPrefix(svc, "real property situated in "):
		p := strings.TrimPrefix(svc, "real property situated in ")
		return []supplyVariant{
			{"ON resident", reg(Supply{ClientProvince: "ON", RecipientResident: true, PropertyProvince: p})},
			{"non-resident individual", reg(Supply{ClientProvince: "US", PropertyProvince: p})},
			{"non-resident business", reg(Supply{ClientProvince: "US", RecipientBusiness: true, PropertyProvince: p})},
		}, true
	case strings.HasPrefix(svc, "litigation in a court/tribunal of "):
		p := strings.TrimSuffix(strings.TrimPrefix(svc, "litigation in a court/tribunal of "), ", after commencement")
		return []supplyVariant{
			{"AB resident", reg(Supply{ClientProvince: "AB", RecipientResident: true, CourtProvince: p})},
			{"ON resident", reg(Supply{ClientProvince: "ON", RecipientResident: true, CourtProvince: p})},
			{"non-resident individual", reg(Supply{ClientProvince: "GB", CourtProvince: p})},
		}, true
	case svc == "litigation in Canada after commencement":
		return []supplyVariant{
			{"ON court", reg(Supply{ClientProvince: "GB", RecipientBusiness: true, CourtProvince: "ON"})},
			{"BC court", reg(Supply{ClientProvince: "GB", RecipientBusiness: true, CourtProvince: "BC"})},
		}, true
	case strings.HasPrefix(svc, "general advice rendered while"):
		return []supplyVariant{{"met in Toronto", reg(Supply{ClientProvince: "US", MetInCanada: true})}}, true
	case strings.HasPrefix(svc, "acting as the client's agent"):
		return []supplyVariant{
			{"individual", Supply{ClientProvince: "US", AgentRole: true, Date: posDate}},
			{"business", Supply{ClientProvince: "US", RecipientBusiness: true, AgentRole: true, Date: posDate}},
		}, true
	case loc == "Canadian resident, no address on file":
		return []supplyVariant{{"resident", reg(Supply{RecipientResident: true})}}, true
	}
	return nil, false
}

// expectedLines turns a row's taxes into the lines Compute must return on tableBase.
func expectedLines(t *testing.T, r tableRow) []Line {
	t.Helper()
	var taxes []tableTax
	if err := json.Unmarshal(r.Taxes, &taxes); err != nil {
		t.Fatalf("taxes %s: %v", r.Taxes, err)
	}
	lines := []Line{}
	for _, x := range taxes {
		if x.RatePPM == 0 {
			continue // "GST/HST" at 0: zero-rated, no line
		}
		l := Line{Component: Component(x.Kind), Jurisdiction: x.Jurisdiction, RatePPM: x.RatePPM, Base: tableBase}
		if x.Kind == "RST" {
			// rule: BIL-T-020
			l.Component, l.Label = PST, "RST"
		}
		amount, err := money.ApplyPPM(tableBase, x.RatePPM)
		if err != nil {
			t.Fatal(err)
		}
		l.Amount = amount
		lines = append(lines, l)
	}
	return lines
}

func TestDecisionTable(t *testing.T) {
	d := NewPlaceOfSupply()
	calc := NewCalculator(NewRateTable(Seed()))
	ran := 0
	for i, r := range loadTable(t) {
		vs, ok := variants(t, r)
		if !ok {
			continue
		}
		ran++
		for _, v := range vs {
			t.Run(r.ServiceType+"/"+r.ClientLocation+"/"+v.name, func(t *testing.T) {
				want := expectedLines(t, r)
				ctx := context.Background()
				if r.Verify {
					// rule: BIL-T-015 (verify)
					// rule: BIL-T-020 (verify)
					if _, err := d.Decide(ctx, v.s); !errors.Is(err, ErrUnverified) {
						t.Fatalf("row %d flag off: err %v, want ErrUnverified", i, err)
					}
					ctx = verifyOn(t)
				}
				p, err := d.Decide(ctx, v.s)
				if err != nil {
					t.Fatalf("row %d Decide: %v", i, err)
				}
				if wantPlace := r.PlaceOfSupply; provinces[wantPlace] || wantPlace == Zero {
					if p.Province != wantPlace {
						t.Fatalf("row %d place %s, want %s", i, p.Province, wantPlace)
					}
				}
				got, err := calc.Compute(ctx, tableBase, p, posDate)
				if err != nil {
					t.Fatalf("row %d Compute: %v", i, err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("row %d lines %+v, want %+v", i, got, want)
				}
			})
		}
	}
	if ran != 44 {
		t.Fatalf("ran %d Decide rows, want 44", ran)
	}
}

// rule: BIL-T-012 (verify)
// The federal-court row: general rule on the client's address, behind the flag.
func TestDecisionTableFederalCourt(t *testing.T) {
	rows := loadTable(t)
	r := rows[44]
	if r.ServiceType != "litigation in a federal court (FC, FCA, TCC, SCC)" || !r.Verify {
		t.Fatalf("row 44 is %q verify=%v", r.ServiceType, r.Verify)
	}
	byClient := map[string]int{}
	for i, g := range rows[:13] {
		byClient[g.ClientLocation] = i
	}
	d := NewPlaceOfSupply()
	calc := NewCalculator(NewRateTable(Seed()))
	for client, gi := range byClient {
		t.Run(client, func(t *testing.T) {
			s := Supply{ClientProvince: client, RecipientResident: true, CourtProvince: FederalCourt, Date: posDate}
			if rows[gi].Verify {
				s.RegisteredProvinces = []string{client}
			}
			if _, err := d.Decide(context.Background(), s); !errors.Is(err, ErrUnverified) {
				t.Fatalf("flag off: err %v, want ErrUnverified", err)
			}
			ctx := verifyOn(t)
			p, err := d.Decide(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			got, err := calc.Compute(ctx, tableBase, p, posDate)
			if err != nil {
				t.Fatal(err)
			}
			if want := expectedLines(t, rows[gi]); !reflect.DeepEqual(got, want) {
				t.Fatalf("lines %+v, want %+v (as general row %d)", got, want, gi)
			}
		})
	}
}

// Rows 45-48 are not place-of-supply decisions on a fee. Disbursements use
// DisbursementProvince here; interest and the card surcharge are decided by
// billing/interest and billing/surcharge, so this test pins only their table content.
func TestDecisionTableOutsideDecide(t *testing.T) {
	rows := loadTable(t)
	calc := NewCalculator(NewRateTable(Seed()))
	ctx := context.Background()
	cases := []struct {
		row     int
		service string
		check   func(t *testing.T, r tableRow)
	}{
		// rule: BIL-T-016
		{45, "agent disbursement line (BIL-T-030 AGENT)", func(t *testing.T, r tableRow) {
			if _, ok := DisbursementProvince("ON", true); ok {
				t.Fatal("agent disbursement has a place of supply")
			}
			if want := expectedLines(t, r); len(want) != 0 {
				t.Fatalf("table taxes %+v, want none", want)
			}
		}},
		// rule: BIL-T-016
		{46, "own-cost disbursement line (BIL-T-030 OWN)", func(t *testing.T, r tableRow) {
			for _, matter := range []Place{hstAt("ON"), gstAt("AB"), {Province: Zero}} {
				prov, ok := DisbursementProvince(matter.Province, false)
				if !ok || prov != matter.Province {
					t.Fatalf("own-cost place %q %v, want matter's %q", prov, ok, matter.Province)
				}
				fee, err := calc.Compute(ctx, tableBase, matter, posDate)
				if err != nil {
					t.Fatal(err)
				}
				own, err := calc.Compute(ctx, tableBase, Place{Province: prov, Components: matter.Components}, posDate)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(own, fee) {
					t.Fatalf("own-cost lines %+v, want matter lines %+v", own, fee)
				}
			}
		}},
		{47, "late-payment interest", func(t *testing.T, r tableRow) {
			if want := expectedLines(t, r); len(want) != 0 || r.Verify {
				t.Fatalf("interest row taxes %+v verify=%v, want none, unflagged", want, r.Verify)
			}
		}},
		{48, "credit card surcharge on a payment", func(t *testing.T, r tableRow) {
			if !r.Verify || r.PlaceOfSupply != "same as the paid bill" {
				t.Fatalf("surcharge row %+v", r)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.service, func(t *testing.T) {
			r := rows[tc.row]
			if r.ServiceType != tc.service {
				t.Fatalf("row %d is %q, want %q", tc.row, r.ServiceType, tc.service)
			}
			tc.check(t, r)
		})
	}
}
