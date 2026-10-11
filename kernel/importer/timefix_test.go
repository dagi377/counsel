package importer

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

func TestMIG_006_InstantsStoredUTC(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Time
		wantErr error
	}{
		{"2026-03-08T04:30:00Z", time.Date(2026, 3, 8, 4, 30, 0, 0, time.UTC), nil},
		{"2026-03-07T23:30:00-05:00", time.Date(2026, 3, 8, 4, 30, 0, 0, time.UTC), nil},
		{"2026-07-01T09:00:00.5-04:00", time.Date(2026, 7, 1, 13, 0, 0, 5e8, time.UTC), nil},
		{"2026-03-08T04:30:00", time.Time{}, ErrBadTime}, // no offset: ambiguous
		{"2026-03-08", time.Time{}, ErrBadTime},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseInstant(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if !got.Equal(tc.want) || got.Location() != time.UTC {
				t.Fatalf("got %v (%v), want %v UTC", got, got.Location(), tc.want)
			}
		})
	}
}

func TestMIG_006_DateOnlyUnshifted(t *testing.T) {
	tests := []struct {
		in      string
		want    clock.Date
		wantErr error
	}{
		{"2026-11-01", clock.Date{Year: 2026, Month: time.November, Day: 1}, nil},
		{"2026-03-08", clock.Date{Year: 2026, Month: time.March, Day: 8}, nil},
		{"2026-01-01", clock.Date{Year: 2026, Month: time.January, Day: 1}, nil},
		{"2026-11-01T00:00:00Z", clock.Date{}, ErrBadTime},
		{"11/01/2026", clock.Date{}, ErrBadTime},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDate(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMIG_006_DerivedDateInBusinessZone(t *testing.T) {
	loc, err := time.LoadLocation(clock.DefaultZone)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in   string
		want clock.Date
	}{
		{"2026-03-08T04:30:00Z", clock.Date{Year: 2026, Month: time.March, Day: 7}}, // UTC-5
		{"2026-07-01T03:59:00Z", clock.Date{Year: 2026, Month: time.June, Day: 30}}, // UTC-4
		{"2026-07-01T04:00:00Z", clock.Date{Year: 2026, Month: time.July, Day: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			ts, err := ParseInstant(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := DerivedDate(ts, loc); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMIG_006_FixTimesRow(t *testing.T) {
	tests := []struct {
		name    string
		fields  map[string]any
		want    map[string]any
		wantErr error
	}{
		{
			name:   "instant and date",
			fields: map[string]any{"at": "2026-03-07T23:30:00-05:00", "due": "2026-11-01", "note": "x"},
			want:   map[string]any{"at": time.Date(2026, 3, 8, 4, 30, 0, 0, time.UTC), "due": clock.Date{Year: 2026, Month: time.November, Day: 1}, "note": "x"},
		},
		{
			name:   "missing and nil fields untouched",
			fields: map[string]any{"due": nil},
			want:   map[string]any{"due": nil},
		},
		{name: "bad instant", fields: map[string]any{"at": "2026-03-08 04:30"}, wantErr: ErrBadTime},
		{name: "date with time part", fields: map[string]any{"due": "2026-11-01T00:00:00Z"}, wantErr: ErrBadTime},
		{name: "not a string", fields: map[string]any{"at": 12}, wantErr: ErrBadTime},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := Row{Resource: "entry", ExternalID: "e1", Fields: tc.fields}
			before := len(tc.fields)
			got, err := FixTimes(in, []string{"at"}, []string{"due"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(tc.fields) != before {
				t.Fatal("input row modified")
			}
			if err != nil {
				return
			}
			if len(got.Fields) != len(tc.want) {
				t.Fatalf("got %v, want %v", got.Fields, tc.want)
			}
			for k, w := range tc.want {
				g := got.Fields[k]
				if wt, ok := w.(time.Time); ok {
					gt, ok := g.(time.Time)
					if !ok || !gt.Equal(wt) || gt.Location() != time.UTC {
						t.Fatalf("%s = %v, want %v UTC", k, g, w)
					}
					continue
				}
				if g != w {
					t.Fatalf("%s = %v, want %v", k, g, w)
				}
			}
			if s, _ := tc.fields["at"].(string); s != "" && tc.fields["at"] == got.Fields["at"] {
				t.Fatal("instant not converted")
			}
		})
	}
}
