package legal

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/registry"
)

// alwaysMethods are the registry calls of the "always" registration set (Q-005): chart of
// accounts, core gate kinds and capabilities. Feature-gated registrations are none of these.
var alwaysMethods = map[string]bool{
	registry.MethodChartOfAccounts: true,
	registry.MethodGateKind:        true,
	registry.MethodCapability:      true,
	registry.MethodRole:            true,
}

func TestRegisterWithNoFeaturesRecordsOnlyAlwaysSet(t *testing.T) {
	tests := []struct {
		name  string
		times int
	}{
		{"once", 1},
		{"twice", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &registry.Recorder{}
			for i := 0; i < tc.times; i++ {
				if err := Register(rec); err != nil {
					t.Fatalf("Register: %v", err)
				}
			}
			for _, c := range rec.Calls() {
				if !alwaysMethods[c.Method] {
					t.Errorf("Register made feature-gated call %s with no features on", c.Method)
				}
			}
			if got, want := len(rec.Method(registry.MethodChartOfAccounts)), 2*tc.times; got != want {
				t.Errorf("ChartOfAccounts calls = %d, want %d (operating and trust per Register)", got, want)
			}
		})
	}
}

func TestVerifyFlagsDefinedOffWithOwnerAndExpiry(t *testing.T) {
	s := flags.New()
	if err := DefineFlags(s); err != nil {
		t.Fatalf("DefineFlags: %v", err)
	}
	defs := map[string]flags.Def{}
	for _, d := range s.Defs() {
		defs[d.Name] = d
	}
	tests := []string{
		"billing.card_surcharge",
		"billing.contingency_fees",
		"billing.credit_route_verified",
		"billing.joint_retainer_verified",
		"billing.receipts_journal_verified",
		"billing.records_verified",
		"billing.solicitors_act_verified",
		"conflict.id_verification",
		"deadline.ontario_pack",
		"export.post_cancel_window",
		"tax.bn_check_verified",
		"tax.verify_rules",
		"timekeeping.paralegal_scope_verified",
		"trust.bylaw9_verified",
	}
	if len(defs) != len(tests) {
		t.Errorf("defined %d flags, want %d", len(defs), len(tests))
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			d, ok := defs[name]
			if !ok {
				t.Fatalf("flag %q not defined", name)
			}
			if s.Enabled(name) {
				t.Errorf("flag %q is on by default", name)
			}
			if d.Owner == "" {
				t.Errorf("flag %q has no owner", name)
			}
			if d.Expiry.IsZero() || d.Expiry.Location().String() != "America/Toronto" {
				t.Errorf("flag %q expiry = %v, want a Toronto date", name, d.Expiry)
			}
			if d.Doc == "" {
				t.Errorf("flag %q has no doc", name)
			}
		})
	}
}

func TestVerifyFlagsNotExpired(t *testing.T) {
	now := time.Now()
	for _, d := range Flags() {
		if !d.Expiry.After(now) {
			t.Errorf("flag %q expired on %s: confirm it and make it permanent, or remove it", d.Name, d.Expiry.Format(time.DateOnly))
		}
	}
}

func TestDefineFlagsTwiceFails(t *testing.T) {
	s := flags.New()
	if err := DefineFlags(s); err != nil {
		t.Fatalf("first DefineFlags: %v", err)
	}
	if err := DefineFlags(s); !errors.Is(err, flags.ErrDuplicate) {
		t.Fatalf("second DefineFlags err = %v, want ErrDuplicate", err)
	}
}

func TestModulesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range modules {
		if m.register == nil {
			t.Errorf("module %q has no register func", m.name)
		}
		if seen[m.name] {
			t.Errorf("module %q listed twice", m.name)
		}
		seen[m.name] = true
	}
	if len(modules) != 36 {
		t.Errorf("modules = %d, want 36 (one per app/legal package with a register.go)", len(modules))
	}
}
