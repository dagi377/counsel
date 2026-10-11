package gate

import (
	"context"
	"errors"
	"testing"
)

func TestARC_I_012_EffectContext(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		wantID GateID
		wantOK bool
	}{
		{"plain context", context.Background(), "", false},
		{"inside an effect", WithEffect(context.Background(), "g-1"), "g-1", true},
		{"empty gate id is not an effect", WithEffect(context.Background(), ""), "", false},
		{"nested keeps the innermost", WithEffect(WithEffect(context.Background(), "g-1"), "g-2"), "g-2", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := InEffect(tc.ctx)
			if id != tc.wantID || ok != tc.wantOK {
				t.Errorf("InEffect = (%q, %v), want (%q, %v)", id, ok, tc.wantID, tc.wantOK)
			}
			err := RequireEffect(tc.ctx)
			if tc.wantOK && err != nil {
				t.Errorf("RequireEffect = %v, want nil", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrOutsideEffect) {
				t.Errorf("RequireEffect = %v, want ErrOutsideEffect", err)
			}
		})
	}
}
