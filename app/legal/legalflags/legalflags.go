// Package legalflags names the app/legal feature flags and is the one way app/legal code
// reads them. It imports only kernel/flags, so every module package can use it without an
// import cycle through app/legal.
package legalflags

import (
	"context"

	"github.com/dagi377/counsel/kernel/flags"
)

// Verify flags gate code built on law nobody has confirmed yet. Each stays off until the owner
// named on it confirms the rule; see docs/DECISIONS-NEEDED.md for the open question behind each.
const (
	CardSurcharge           = "billing.card_surcharge"               // V-SURCHARGE
	ContingencyFees         = "billing.contingency_fees"             // V-CFA
	CreditRouteVerified     = "billing.credit_route_verified"        // V-TRUST
	JointRetainerVerified   = "billing.joint_retainer_verified"      // V-BILL
	ReceiptsJournalVerified = "billing.receipts_journal_verified"    // V-BILL
	BillingRecordsVerified  = "billing.records_verified"             // V-TRUST
	SolicitorsActVerified   = "billing.solicitors_act_verified"      // V-BILL
	IDVerification          = "conflict.id_verification"             // V-ID
	DeadlineOntarioPack     = "deadline.ontario_pack"                // V-DEADLINES
	ExportPostCancelWindow  = "export.post_cancel_window"            // customer contract
	TaxBNCheckVerified      = "tax.bn_check_verified"                // V-TAX
	TaxVerifyRules          = "tax.verify_rules"                     // V-TAX
	ParalegalScopeVerified  = "timekeeping.paralegal_scope_verified" // V-SCOPE
	TrustBylaw9Verified     = "trust.bylaw9_verified"                // V-TRUST
)

// Enabled reports whether flag name is on in the flag set the request context carries. It is
// false when the flag is undefined, not turned on, or ctx carries no set.
func Enabled(ctx context.Context, name string) bool {
	return flags.Enabled(ctx, name)
}
