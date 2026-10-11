package limitationfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/limitation"
)

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestFakeCompute(t *testing.T) {
	f := New()
	f.NoPeriod = []string{"none"}
	tests := []struct {
		name string
		a    limitation.Assessment
		want []time.Time
		err  error
	}{
		{"basic and ultimate", limitation.Assessment{MatterID: "m", ClaimType: "tort", ActDate: day(2020, 1, 1), DiscoveryDate: day(2021, 3, 1)},
			[]time.Time{*day(2023, 3, 1), *day(2035, 1, 1)}, nil},
		{"suspension extends ultimate", limitation.Assessment{MatterID: "m", ClaimType: "tort", ActDate: day(2020, 1, 1), DiscoveryDate: day(2020, 1, 1),
			Suspensions: []limitation.Suspension{{Kind: limitation.SuspendMinority, From: *day(2020, 1, 1), To: day(2020, 1, 11)}}},
			[]time.Time{*day(2022, 1, 1), *day(2035, 1, 11)}, nil},
		{"open suspension: basic only", limitation.Assessment{LeadID: "l", ClaimType: "tort", ActDate: day(2020, 1, 1), DiscoveryDate: day(2020, 1, 1),
			Suspensions: []limitation.Suspension{{Kind: limitation.SuspendIncapacity, From: *day(2020, 1, 1)}}},
			[]time.Time{*day(2022, 1, 1)}, nil},
		{"discovery before act", limitation.Assessment{MatterID: "m", ActDate: day(2020, 1, 2), DiscoveryDate: day(2020, 1, 1)}, nil, limitation.ErrDates},
		{"missing dates", limitation.Assessment{MatterID: "m"}, nil, limitation.ErrDates},
		{"both owners", limitation.Assessment{MatterID: "m", LeadID: "l"}, nil, limitation.ErrOwner},
		{"no period", limitation.Assessment{MatterID: "m", ClaimType: "none"}, nil, limitation.ErrNoPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Compute(context.Background(), tt.a)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if !got[i].Due.Equal(tt.want[i]) {
					t.Errorf("%s = %v, want %v", got[i].Kind, got[i].Due, tt.want[i])
				}
			}
		})
	}
}

func TestFakeConfirm(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		by   []string
		err  error
	}{
		{"other user confirms", []string{"u2"}, nil},
		{"preparer cannot confirm", []string{"u1"}, limitation.ErrSelfConfirm},
		{"empty confirmer", []string{""}, limitation.ErrSelfConfirm},
		{"twice", []string{"u2", "u3"}, limitation.ErrConfirmed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			a, err := f.Create(ctx, limitation.Assessment{MatterID: "m1", ClaimType: "tort", PreparedBy: "u1"})
			if err != nil {
				t.Fatal(err)
			}
			for _, by := range tt.by {
				_, err = f.Confirm(ctx, a.ID, by)
			}
			if !errors.Is(err, tt.err) {
				t.Errorf("Confirm() = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestFakeStore(t *testing.T) {
	ctx := context.Background()
	f := New()
	tests := []struct {
		name string
		a    limitation.Assessment
		err  error
	}{
		{"matter", limitation.Assessment{MatterID: "m1"}, nil},
		{"lead", limitation.Assessment{LeadID: "l1"}, nil},
		{"neither", limitation.Assessment{}, limitation.ErrOwner},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := f.Create(ctx, tt.a); !errors.Is(err, tt.err) {
				t.Errorf("Create() = %v, want %v", err, tt.err)
			}
		})
	}
	if got, _ := f.ForMatter(ctx, "m1"); len(got) != 1 {
		t.Errorf("ForMatter(m1) = %v", got)
	}
	if _, err := f.Get(ctx, "nope"); !errors.Is(err, limitation.ErrNotFound) {
		t.Errorf("Get(nope) = %v", err)
	}
}
