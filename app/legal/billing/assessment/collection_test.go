package assessment

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/interest/interestfake"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

// rule: BIL-L-004 (verify)
func TestBIL_L_004_CollectionHeldOneMonth(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02T15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	tests := []struct {
		name      string
		delivered time.Time
		today     string
		action    CollectionAction
		flagOn    bool
		want      error
	}{
		{"delivered 01-28, suit on 02-28 blocked", at("2026-01-28T10:00"), "2026-02-28T12:00", ActionLegal, true, ErrTooEarly},
		{"delivered 01-28, suit on 03-01 allowed", at("2026-01-28T10:00"), "2026-03-01T09:00", ActionLegal, true, nil},
		{"claim referral blocked the same way", at("2026-01-28T10:00"), "2026-02-28T12:00", ActionClaimReferral, true, ErrTooEarly},
		{"delivered 01-31 clamps to 02-28", at("2026-01-31T10:00"), "2026-02-28T23:59", ActionLegal, true, ErrTooEarly},
		{"delivered 01-31 allowed 03-01", at("2026-01-31T10:00"), "2026-03-01T00:00", ActionLegal, true, nil},
		{"late-evening delivery uses Toronto date", at("2026-10-31T23:30"), "2026-11-30T12:00", ActionLegal, true, ErrTooEarly},
		{"late-evening delivery allowed 12-01", at("2026-10-31T23:30"), "2026-12-01T00:00", ActionLegal, true, nil},
		{"reminder never held", at("2026-01-28T10:00"), "2026-01-29T10:00", ActionReminder, true, nil},
		{"flag off disables suits", at("2026-01-28T10:00"), "2026-06-01T10:00", ActionLegal, false, ErrCollectionUnverified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := CollectionHold{Calendar: interestfake.New(loc), Clock: clockfake.New(at(tt.today), loc)}
			ctx := legalflagstest.Context(t, FlagSolicitorsActVerified, tt.flagOn)
			if err := h.Check(ctx, tt.action, tt.delivered.UTC()); !errors.Is(err, tt.want) {
				t.Errorf("Check = %v, want %v", err, tt.want)
			}
		})
	}
}
