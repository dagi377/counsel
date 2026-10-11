package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestLegalEffects(t *testing.T) {
	fx := legalEffects()
	if len(fx) != 19 {
		t.Fatalf("legalEffects() has %d kinds, want 19", len(fx))
	}
	for k, f := range fx {
		if f == nil {
			t.Errorf("%s: nil effect", k)
		}
	}
	if _, err := withLegalEffects(map[string]registry.EffectFunc{"conflict.wall": ApprovalEffect}); !errors.Is(err, ErrDuplicateEffect) {
		t.Fatalf("duplicate kind: err = %v, want ErrDuplicateEffect", err)
	}
}

func TestDraftOr(t *testing.T) {
	errDomain := errors.New("domain effect ran")
	domain := func(context.Context, pgx.Tx, registry.Approved) error { return errDomain }
	tests := []struct {
		name    string
		subject string
		want    error
	}{
		// rule: ARC-I-019
		{"agent draft takes the approval effect", SubjectDocumentDraft, gates.ErrAgentApproves},
		{"any other subject takes the domain effect", "deadline", errDomain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := approved("cal.change", tt.subject, fxPreparer, agentRun)
			if err := draftOr(domain)(context.Background(), nil, g); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
