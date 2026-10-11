package party_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/dagi377/counsel/app/legal/party"
)

const dataModel = "../../../spec/eng/a4-data-model.json"

// a4Columns returns the a4 column names of table, in order.
func a4Columns(t *testing.T, table string) []string {
	t.Helper()
	raw, err := os.ReadFile(dataModel)
	if err != nil {
		t.Fatalf("read data model: %v", err)
	}
	var dm struct {
		Tables []struct {
			Name    string `json:"name"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(raw, &dm); err != nil {
		t.Fatalf("parse data model: %v", err)
	}
	for _, tb := range dm.Tables {
		if tb.Name == table {
			out := make([]string, 0, len(tb.Columns))
			for _, c := range tb.Columns {
				out = append(out, c.Name)
			}
			return out
		}
	}
	t.Fatalf("table %s not in data model", table)
	return nil
}

// snake turns a Go field name into its column name (FromContactID -> from_contact_id).
func snake(s string) string {
	s = strings.ReplaceAll(s, "ID", "Id")
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestFieldListsMatchA4(t *testing.T) {
	tests := []struct {
		table string
		list  []string
		row   any
	}{
		{"party.contact", party.ContactColumns, party.Contact{}},
		{"party.contact_relationship", party.RelationshipColumns, party.Relationship{}},
	}
	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			want := a4Columns(t, tt.table)
			if !slices.Equal(tt.list, want) {
				t.Fatalf("field list = %v\nwant a4 columns %v", tt.list, want)
			}
			rt := reflect.TypeOf(tt.row)
			for i := 0; i < rt.NumField(); i++ {
				col := snake(rt.Field(i).Name)
				if !slices.Contains(want, col) {
					t.Errorf("%s.%s -> %q is not an a4 column of %s", rt.Name(), rt.Field(i).Name, col, tt.table)
				}
			}
		})
	}
}

func TestRelationshipValidate(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC) }
	ok := party.Relationship{FromContactID: "c1", ToContactID: "c2", Kind: party.DirectorOf}
	tests := []struct {
		name    string
		mut     func(r *party.Relationship)
		wantErr error
	}{
		{"valid", func(*party.Relationship) {}, nil},
		{"missing contact", func(r *party.Relationship) { r.ToContactID = "" }, party.ErrInvalid},
		{"self", func(r *party.Relationship) { r.ToContactID = r.FromContactID }, party.ErrInvalid},
		{"unknown kind", func(r *party.Relationship) { r.Kind = "cousin" }, party.ErrInvalid},
		{"ends before start", func(r *party.Relationship) { r.StartsOn, r.EndsOn = day(5), day(4) }, party.ErrInvalid},
		{"open-ended", func(r *party.Relationship) { r.StartsOn = day(5) }, nil},
		{"confirmed by without at", func(r *party.Relationship) { r.ConfirmedBy = "u1" }, party.ErrInvalid},
		{"confirmed", func(r *party.Relationship) { r.ConfirmedBy, r.ConfirmedAt = "u1", day(6) }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ok
			tt.mut(&r)
			if err := r.Validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRelationshipConfirmed(t *testing.T) {
	tests := []struct {
		name string
		r    party.Relationship
		want bool
	}{
		{"agent proposal", party.Relationship{ProposedByAgent: true}, false},
		{"confirmed", party.Relationship{ProposedByAgent: true, ConfirmedBy: "u1", ConfirmedAt: time.Unix(1, 0)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.Confirmed(); got != tt.want {
				t.Fatalf("Confirmed() = %v, want %v", got, tt.want)
			}
		})
	}
}
