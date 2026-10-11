package numseqfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/numseq"
)

func TestFakeNext(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		format  string
		calls   int
		newYear bool
		want    string
	}{
		{"year and seq", "{YYYY}-{SEQ:4}", 3, false, "2026-0003"},
		{"plain seq", "INV{SEQ:6}", 2, false, "INV000002"},
		{"year resets", "{YYYY}-{SEQ:4}", 2, true, "2027-0001"},
		{"no year keeps counting", "{SEQ:3}", 2, true, "003"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := clockfake.New(time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC), nil)
			f := New(clk)
			if err := f.Define(ctx, numseq.Series{Name: "s", Format: tt.format}); err != nil {
				t.Fatal(err)
			}
			var got string
			var err error
			for i := 0; i < tt.calls; i++ {
				if got, err = f.Next(ctx, nil, "s"); err != nil {
					t.Fatal(err)
				}
			}
			if tt.newYear {
				clk.Advance(24 * time.Hour)
				if got, err = f.Next(ctx, nil, "s"); err != nil {
					t.Fatal(err)
				}
			}
			if got != tt.want {
				t.Fatalf("Next = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFakeErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		define []numseq.Series
		next   string
		want   error
	}{
		{"unknown series", nil, "missing", numseq.ErrUnknownSeries},
		{"no seq token", []numseq.Series{{Name: "a", Format: "{YYYY}"}}, "", numseq.ErrBadFormat},
		{"unknown token", []numseq.Series{{Name: "a", Format: "{X}-{SEQ:2}"}}, "", numseq.ErrBadFormat},
		{"conflict", []numseq.Series{{Name: "a", Format: "{SEQ:2}"}, {Name: "a", Format: "{SEQ:3}"}}, "", numseq.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(clockfake.New(time.Unix(0, 0), nil))
			var err error
			for _, s := range tt.define {
				if err = f.Define(ctx, s); err != nil {
					break
				}
			}
			if tt.next != "" {
				_, err = f.Next(ctx, nil, tt.next)
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
