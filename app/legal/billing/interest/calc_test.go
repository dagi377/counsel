package interest

import (
	"errors"
	"math"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

func TestCalcSimple(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	tests := []struct {
		name      string
		principal money.Cents
		rate      money.PPM
		days      int
		want      money.Cents
		wantErr   error
	}{
		{"accept 31 days at 12%", 100000, 120000, 31, 1019, nil},
		{"zero days", 100000, 120000, 0, 0, nil},
		{"zero rate", 100000, 0, 31, 0, nil},
		{"full year", 100000, 120000, 365, 12000, nil},
		{"rounds half up", 1, 182500, 1000, 1, nil},
		{"overflow", math.MaxInt64 / 2, 120000, 31, 0, money.ErrOverflow},
		{"negative days", 100000, 120000, -1, 0, ErrBadPeriod},
		{"negative principal", -1, 120000, 1, 0, ErrBadPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Simple(tt.principal, tt.rate, tt.days)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Simple = %d, want %d", got, tt.want)
			}
		})
	}
}
