package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestKernelAdminGates(t *testing.T) {
	noop := func(context.Context, pgx.Tx, registry.Approved) error { return nil }
	tests := []struct {
		name     string
		opts     []identity.AdminOption
		wantErr  error
		wantKind string
	}{
		{"kernel kinds only", nil, nil, identity.KindRoleChange},
		{"app core effect added", []identity.AdminOption{identity.WithCoreEffect("core.example", noop)}, nil, "core.example"},
		{"non-core kind refused", []identity.AdminOption{identity.WithCoreEffect("desk.example", noop)}, identity.ErrInvalidCoreEffect, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &Kernel{}
			ag, err := k.AdminGates(tt.opts...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AdminGates() = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if ag.Effects()[tt.wantKind] == nil {
				t.Fatalf("Effects() has no %s", tt.wantKind)
			}
			if k.Admin.Effects()[tt.wantKind] == nil {
				t.Fatalf("k.Admin not kept: no %s", tt.wantKind)
			}
		})
	}
	var nilK *Kernel
	if _, err := nilK.AdminGates(); !errors.Is(err, ErrConfig) {
		t.Fatalf("nil kernel AdminGates() = %v, want %v", err, ErrConfig)
	}
}
