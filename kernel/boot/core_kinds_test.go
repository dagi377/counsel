package boot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

func coreKind(slug string) registry.GateKind {
	return registry.GateKind{
		Slug: slug, Approver: "admin.offboard", Risk: gate.RiskHigh, MinApprovers: 2, TTL: 168 * time.Hour,
		PayloadSchema: []byte(`{"type":"object"}`),
		Effect:        func(context.Context, pgx.Tx, registry.Approved) error { return nil },
	}
}

// rule: ARC-I-013
func TestCoreGateKind(t *testing.T) {
	tests := []struct {
		name    string
		kinds   []registry.GateKind
		wantErr error
		want    int
	}{
		{"core kind recorded", []registry.GateKind{coreKind("core.offboarding")}, nil, 1},
		{"app kind refused", []registry.GateKind{coreKind("app.thing")}, gate.ErrInvalidKind, 0},
		{"kernel. prefix refused", []registry.GateKind{coreKind("kernel.thing")}, gate.ErrInvalidKind, 0},
		{"duplicate refused", []registry.GateKind{coreKind("core.offboarding"), coreKind("core.offboarding")}, gate.ErrDuplicateKind, 1},
		{"no effect refused", []registry.GateKind{func() registry.GateKind { g := coreKind("core.offboarding"); g.Effect = nil; return g }()}, gate.ErrInvalidKind, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &Kernel{}
			var err error
			for _, g := range tt.kinds {
				if err = k.CoreGateKind(g); err != nil {
					break
				}
			}
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := len(k.CoreGateKinds()); got != tt.want {
				t.Fatalf("recorded %d, want %d", got, tt.want)
			}
		})
	}
}

func TestApplyCoreKinds(t *testing.T) {
	fake := gatefake.New(time.Now, gatefake.Caps{})
	k := &Kernel{Gates: fake}
	if err := k.CoreGateKind(coreKind("core.offboarding")); err != nil {
		t.Fatal(err)
	}
	if err := applyCoreKinds(k); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// A second registration of the same kind is the engine's duplicate.
	if err := applyCoreKinds(k); !errors.Is(err, gate.ErrDuplicateKind) {
		t.Fatalf("re-apply err = %v, want ErrDuplicateKind", err)
	}
	pg := gate.NewPG()
	if err := pg.RegisterCore(coreKind("core.offboarding")); err != nil {
		t.Fatalf("PG RegisterCore: %v", err)
	}
	if err := pg.Register(coreKind("core.second_kind")); !errors.Is(err, gate.ErrInvalidKind) {
		t.Fatalf("PG Register core kind err = %v, want ErrInvalidKind", err)
	}
}
