package apifake

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/api"
)

func TestFakeCursorRoundTrip(t *testing.T) {
	f := New(50, 200)
	tests := []struct {
		name   string
		cursor api.Cursor
		tamper func(string) string
		want   error
	}{
		{"round trip", api.Cursor{After: "0190-abc", Limit: 25}, nil, nil},
		{"empty after", api.Cursor{Limit: 1}, nil, nil},
		{"tampered body", api.Cursor{After: "x", Limit: 2}, func(s string) string { return "A" + s[1:] }, api.ErrBadCursor},
		{"no signature", api.Cursor{After: "x"}, func(s string) string { return s[:len(s)-44] }, api.ErrBadCursor},
		{"garbage", api.Cursor{}, func(string) string { return "%%%" }, api.ErrBadCursor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := f.Encode(tt.cursor)
			if err != nil {
				t.Fatal(err)
			}
			if tt.tamper != nil {
				s = tt.tamper(s)
			}
			got, err := f.Decode(s)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Decode err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && got != tt.cursor {
				t.Fatalf("Decode = %+v, want %+v", got, tt.cursor)
			}
		})
	}
}

func TestFakeClamp(t *testing.T) {
	f := New(50, 200)
	tests := []struct{ in, want int }{{0, 50}, {-3, 50}, {10, 10}, {200, 200}, {201, 200}}
	for _, tt := range tests {
		if got := f.Clamp(tt.in); got != tt.want {
			t.Errorf("Clamp(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestProblemError(t *testing.T) {
	var err error = &api.Problem{Title: "Not found", Status: 404, Code: "not_found"}
	var p *api.Problem
	if !errors.As(err, &p) || p.Status != 404 || err.Error() != "not_found: Not found" {
		t.Fatalf("problem as error: %v", err)
	}
}
