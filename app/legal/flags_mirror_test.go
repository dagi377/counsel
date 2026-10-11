package legal

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/assessment"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/split"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/billing/trustapply"
	"github.com/dagi377/counsel/app/legal/clio"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/export"
	"github.com/dagi377/counsel/app/legal/reports"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust"
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
		{"tax bn", tax.FlagBNCheckVerified, FlagTaxBNCheckVerified},
		{"credit", credit.FlagCreditRouteVerified, FlagCreditRouteVerified},
		{"split", split.FlagJointRetainerVerified, FlagJointRetainerVerified},
		{"trust", trust.FlagBylaw9Verified, FlagTrustBylaw9Verified},
		{"clio", clio.FlagBylaw9Verified, FlagTrustBylaw9Verified},
		{"reports trust listing", reports.FlagBylaw9Verified, FlagTrustBylaw9Verified},
		{"reports billing records", reports.FlagBillingRecordsVerified, FlagBillingRecordsVerified},
		{"assessment", assessment.FlagSolicitorsActVerified, FlagSolicitorsActVerified},
		{"conflict", conflict.FlagIDVerification, FlagIDVerification},
		{"export", export.FlagPostCancelWindow, FlagExportPostCancelWindow},
		{"interest", interest.FlagSolicitorsActVerified, FlagSolicitorsActVerified},
		{"payment receipts journal", payment.FlagReceiptsJournalVerified, FlagReceiptsJournalVerified},
		{"timekeeping paralegal scope", timekeeping.FlagParalegalScopeVerified, FlagParalegalScopeVerified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mirror != tt.want {
				t.Fatalf("mirror = %q, want %q", tt.mirror, tt.want)
			}
		})
	}
}
