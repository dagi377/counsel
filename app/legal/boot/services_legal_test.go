package boot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/limitation"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/signatory/signatoryfake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

func actDate(t *testing.T, s string) *time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatal(err)
	}
	return &d
}

// Legislation Act, 2006 s.87 and s.88 (verify): the boot-built Statute carries the s.87
// calendar, so a basic period whose last day is an s.87 holiday rolls to the next day that
// is not a holiday. The r. 1.03 court substitutes (Saturday-substitute Mondays and the
// Christmas Monday and Tuesday) do not move a limitation date (review B3).
// rule: Legislation Act, 2006 s.87, s.88 (verify)
func TestLimitationStatuteRollsHolidayForward(t *testing.T) {
	ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
	tests := []struct {
		name  string
		act   string
		basic string
	}{
		{"canada day 2027 rolls to friday", "2025-07-01", "2027-07-02"},
		{"thanksgiving 2027 rolls to tuesday", "2025-10-11", "2027-10-12"},
		{"business day stays", "2025-07-02", "2027-07-02"},
		{"ordinary saturday stands", "2026-07-08", "2028-07-08"},
		{"monday after saturday canada day 2028 stays (B3)", "2026-07-03", "2028-07-03"},
		{"monday after saturday new year 2028 stays (B3)", "2026-01-03", "2028-01-03"},
		{"canada day 2028 on a saturday is itself a holiday: rolls to monday", "2026-07-01", "2028-07-03"},
		{"canada day 2029 on a sunday rolls past the monday substitute", "2027-07-01", "2029-07-03"},
		{"new year 2034 on a sunday rolls past the monday substitute", "2032-01-01", "2034-01-03"},
		{"remembrance day 2028 on a saturday rolls to monday", "2026-11-11", "2028-11-13"},
		{"monday after saturday remembrance day 2028 stays", "2026-11-13", "2028-11-13"},
		{"christmas 2027 on a saturday rolls to monday, not past the court tuesday", "2025-12-25", "2027-12-27"},
		{"boxing day 2027 on a sunday rolls to monday", "2025-12-26", "2027-12-27"},
		{"court christmas tuesday 2027 stays", "2025-12-28", "2027-12-28"},
		{"christmas 2033 on a sunday rolls past boxing day and the tuesday", "2031-12-25", "2033-12-28"},
		{"boxing day 2026 on a saturday rolls to monday", "2024-12-26", "2026-12-28"},
		{"court monday after friday christmas 2026 stays", "2024-12-28", "2026-12-28"},
		{"good friday 2028 rolls to saturday", "2026-04-14", "2028-04-15"},
		{"easter monday 2028 rolls to tuesday", "2026-04-17", "2028-04-18"},
	}
	// The boot calendar covers a window rolling with today; the table pins 2024-2035 so the
	// golden dates do not age out. TestLimitationStatuteUsesLegislationActCalendar checks
	// boot attaches the same calendar.
	base, err := limitation.NewStatute(nil)
	if err != nil {
		t.Fatalf("NewStatute: %v", err)
	}
	s := base.WithHolidays(ontario.LegislationActCalendar().Holidays(2024, 2035), 2024, 2035)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps, err := s.Compute(ctx, limitation.Assessment{MatterID: "m", ActDate: actDate(t, tt.act), DiscoveryPresumed: true})
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			var basic string
			for _, p := range ps {
				if p.Kind == "basic" {
					basic = p.Due.Format(time.DateOnly)
				}
			}
			if basic != tt.basic {
				t.Fatalf("basic = %q, want %s (periods %v)", basic, tt.basic, ps)
			}
		})
	}
}

// The boot Statute rolls past next year's dated s.87 holidays but not past the r. 1.03
// court-only substitutes (review B3), so boot attaches the Legislation Act calendar.
// rule: Legislation Act, 2006 s.87, s.88 (verify)
func TestLimitationStatuteUsesLegislationActCalendar(t *testing.T) {
	ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
	s, err := LimitationStatute(nil)
	if err != nil {
		t.Fatalf("LimitationStatute: %v", err)
	}
	y := time.Now().Year() + 1
	s87 := map[clock.Date]bool{}
	for _, h := range ontario.LegislationActCalendar().Holidays(y, y) {
		s87[h] = true
	}
	for _, h := range ontario.CourtCalendar().Holidays(y, y) {
		wd := h.Start(time.UTC).Weekday()
		if wd == time.Saturday || wd == time.Sunday || (h.Month == time.February && h.Day == 29) {
			continue
		}
		t.Run(h.String(), func(t *testing.T) {
			act := time.Date(h.Year-2, h.Month, h.Day, 0, 0, 0, 0, time.UTC)
			ps, err := s.Compute(ctx, limitation.Assessment{MatterID: "m", ActDate: &act, DiscoveryPresumed: true})
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			var basic string
			for _, p := range ps {
				if p.Kind == "basic" {
					basic = p.Due.Format(time.DateOnly)
				}
			}
			rolled := basic != h.String()
			if rolled != s87[h] {
				t.Fatalf("basic = %s for last day %s: rolled %v, want %v (s.87 holiday %v)", basic, h, rolled, s87[h], s87[h])
			}
		})
	}
}

// The core.trust_signatory effect reaches kernel admin gates through WithCoreEffect, and the
// handler it installs is the signatory change effect.
func TestCoreGateEffectsSuppliesTrustSignatory(t *testing.T) {
	ag, err := identity.NewAdminGates(nil, nil, CoreGateEffects(signatoryfake.New())...)
	if err != nil {
		t.Fatalf("NewAdminGates: %v", err)
	}
	effects := ag.Effects()
	fn := effects[signatory.KindChange]
	if fn == nil {
		t.Fatalf("no effect for %s in %v", signatory.KindChange, effects)
	}
	for _, k := range []string{identity.KindRoleChange, identity.KindUserActivation} {
		if effects[k] == nil {
			t.Fatalf("kernel admin kind %s lost its effect", k)
		}
	}
	bad, _ := json.Marshal(map[string]string{"change": "add"})
	tests := []struct {
		name string
		g    registry.Approved
		want error
	}{
		{"alias kind never applies", registry.Approved{GateID: "g1", Kind: signatory.KindSigningAuthority}, signatory.ErrAliasKind},
		{"invalid payload", registry.Approved{GateID: "g2", Kind: signatory.KindChange, Payload: bad}, signatory.ErrInvalidPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := fn(context.Background(), pgx.Tx(nil), tt.g); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
