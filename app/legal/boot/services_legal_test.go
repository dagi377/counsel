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
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/signatory/signatoryfake"
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

// Legislation Act, 2006 s.88 (verify): the boot-built Statute carries the Ontario calendar,
// so a basic period whose last day is an Ontario holiday rolls to the next business day.
func TestLimitationStatuteRollsHolidayForward(t *testing.T) {
	ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
	tests := []struct {
		name  string
		act   string
		basic string
	}{
		{"canada day 2027 rolls to friday", "2025-07-01", "2027-07-02"},
		{"thanksgiving 2027 rolls to tuesday", "2025-10-11", "2027-10-12"},
		{"christmas 2027 weekend and both substitutes roll to wednesday", "2025-12-25", "2027-12-29"},
		{"business day stays", "2025-07-02", "2027-07-02"},
	}
	s, err := LimitationStatute(nil)
	if err != nil {
		t.Fatalf("LimitationStatute: %v", err)
	}
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
