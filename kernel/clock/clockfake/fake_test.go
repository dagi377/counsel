package clockfake

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

func TestFakeToday(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	tests := []struct {
		name    string
		now     time.Time
		loc     *time.Location
		advance time.Duration
		want    clock.Date
	}{
		{"utc", time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC), nil, 0, clock.Date{Year: 2026, Month: 11, Day: 1}},
		{"toronto previous day", time.Date(2026, 11, 1, 3, 30, 0, 0, time.UTC), toronto, 0, clock.Date{Year: 2026, Month: 10, Day: 31}},
		{"advance across midnight", time.Date(2026, 10, 31, 3, 30, 0, 0, time.UTC), toronto, 24 * time.Hour, clock.Date{Year: 2026, Month: 10, Day: 31}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(tt.now, tt.loc)
			f.Advance(tt.advance)
			if got := f.Today(context.Background()); got != tt.want {
				t.Fatalf("Today() = %+v, want %+v", got, tt.want)
			}
			if want := tt.now.Add(tt.advance); !f.Now().Equal(want) {
				t.Fatalf("Now() = %v, want %v", f.Now(), want)
			}
		})
	}
}

func TestFakeSet(t *testing.T) {
	f := New(time.Unix(0, 0), nil)
	at := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	f.Set(at)
	if !f.Now().Equal(at) || f.Location() != time.UTC {
		t.Fatalf("Set: now=%v loc=%v", f.Now(), f.Location())
	}
}
