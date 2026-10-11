package legal

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/billing/trustapply"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// Module packages that app/legal imports cannot import app/legal back, so they mirror the
// flag names they read. This test keeps every mirror equal to the canonical name.
func TestFlagMirrorsMatchCanonicalNames(t *testing.T) {
	tests := []struct {
		name   string
		mirror string
		want   string
	}{
		{"withdraw", withdraw.FlagBylaw9Verified, FlagTrustBylaw9Verified},
		{"trustapply", trustapply.FlagBylaw9Verified, FlagTrustBylaw9Verified},
		{"tax", tax.FlagVerifyRules, FlagTaxVerifyRules},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mirror != tt.want {
				t.Fatalf("mirror = %q, want %q", tt.mirror, tt.want)
			}
		})
	}
}
