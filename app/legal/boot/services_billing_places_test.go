package boot

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
)

// placeMatters is a scripted MatterReader.
type placeMatters map[matter.ID]matter.Matter

func (m placeMatters) Get(_ context.Context, id matter.ID) (matter.Matter, error) {
	got, ok := m[id]
	if !ok {
		return matter.Matter{}, matter.ErrNotFound
	}
	return got, nil
}

// placeRow scans scripted values into strings and bools.
type placeRow struct {
	vals []any
	err  error
}

func (r placeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = r.vals[i].(string)
		case *bool:
			*p = r.vals[i].(bool)
		}
	}
	return nil
}

// placeTx answers the client-address and firm-registration reads.
type placeTx struct {
	pgx.Tx
	client, firm placeRow
}

func (t placeTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "core.firm") {
		return t.firm
	}
	return t.client
}

// rule: BIL-T-010
// The place-of-supply input reads the client's billing address, the matter's court and
// property provinces and the firm's provincial registrations.
func TestBIL_T_010_ClientPlacesReadsTaxFields(t *testing.T) {
	ms := placeMatters{
		"plain":  {ID: "plain", ClientID: "c1"},
		"court":  {ID: "court", ClientID: "c1", CourtProvince: "ns", PropertyProvince: " bc "},
		"orphan": {ID: "orphan"},
	}
	onBilling := placeRow{vals: []any{"ON", "CA", false}}
	noFirm := placeRow{err: pgx.ErrNoRows}
	tests := []struct {
		name, matter string
		client, firm placeRow
		want         tax.Supply
		err          error
	}{
		{"billing address province, no registrations", "plain", onBilling, noFirm,
			tax.Supply{ClientProvince: "ON", RecipientResident: true, ServiceType: "general"}, nil},
		{"court and property provinces from the matter", "court", onBilling, placeRow{vals: []any{""}},
			tax.Supply{ClientProvince: "ON", RecipientResident: true, ServiceType: "general", CourtProvince: "NS", PropertyProvince: "BC"}, nil},
		{"firm registrations", "plain", onBilling, placeRow{vals: []any{"MB,BC"}},
			tax.Supply{ClientProvince: "ON", RecipientResident: true, ServiceType: "general", RegisteredProvinces: []string{"MB", "BC"}}, nil},
		{"foreign organization is a non-resident business", "plain", placeRow{vals: []any{"", "GB", true}}, noFirm,
			tax.Supply{ClientProvince: "GB", RecipientBusiness: true, ServiceType: "general"}, nil},
		{"unknown matter fails closed", "missing", onBilling, noFirm, tax.Supply{}, matter.ErrNotFound},
		{"matter without client fails closed", "orphan", onBilling, noFirm, tax.Supply{}, party.ErrNotFound},
		{"client row missing fails closed", "plain", placeRow{err: pgx.ErrNoRows}, noFirm, tax.Supply{}, party.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClientPlaces{Matters: ms}.Supply(context.Background(), placeTx{client: tt.client, firm: tt.firm}, tt.matter)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("supply = %+v, want %+v", got, tt.want)
			}
		})
	}
	if _, err := (ClientPlaces{}).Supply(context.Background(), placeTx{}, "plain"); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("nil reader: err = %v", err)
	}
}

// rule: BIL-T-010
// The billing address is preferred over the first address, and the SQL reads it by type.
func TestClientPlacesBillingAddressSQL(t *testing.T) {
	for _, want := range []string{"'billing' DESC", "WITH ORDINALITY", "party.contact"} {
		if !strings.Contains(clientPlaceSQL, want) {
			t.Fatalf("clientPlaceSQL lacks %q", want)
		}
	}
	if !strings.Contains(firmRegistrationsSQL, "tax_registration_provinces") {
		t.Fatal("firm registrations SQL does not read tax_registration_provinces")
	}
	var _ draft.Places = ClientPlaces{}
}
