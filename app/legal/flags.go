package legal

import (
	"fmt"
	"time"
	_ "time/tzdata" // the expiry is a Toronto date on any host

	"github.com/dagi377/counsel/kernel/flags"
)

// Verify flags gate code built on law nobody has confirmed yet. Each stays off until the owner
// named on it confirms the rule; see docs/DECISIONS-NEEDED.md for the open question behind each.
const (
	FlagCardSurcharge           = "billing.card_surcharge"               // V-SURCHARGE
	FlagContingencyFees         = "billing.contingency_fees"             // V-CFA
	FlagCreditRouteVerified     = "billing.credit_route_verified"        // V-TRUST
	FlagJointRetainerVerified   = "billing.joint_retainer_verified"      // V-BILL
	FlagReceiptsJournalVerified = "billing.receipts_journal_verified"    // V-BILL
	FlagBillingRecordsVerified  = "billing.records_verified"             // V-TRUST
	FlagSolicitorsActVerified   = "billing.solicitors_act_verified"      // V-BILL
	FlagIDVerification          = "conflict.id_verification"             // V-ID
	FlagDeadlineOntarioPack     = "deadline.ontario_pack"                // V-DEADLINES
	FlagExportPostCancelWindow  = "export.post_cancel_window"            // customer contract
	FlagTaxBNCheckVerified      = "tax.bn_check_verified"                // V-TAX
	FlagTaxVerifyRules          = "tax.verify_rules"                     // V-TAX
	FlagParalegalScopeVerified  = "timekeeping.paralegal_scope_verified" // V-SCOPE
	FlagTrustBylaw9Verified     = "trust.bylaw9_verified"                // V-TRUST
)

// verifyExpiry is when every verify flag must be confirmed and made permanent, or removed.
var verifyExpiry = time.Date(2027, time.June, 30, 0, 0, 0, 0, toronto())

// toronto returns the business time zone. tzdata is embedded, so loading cannot fail.
func toronto() *time.Location {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		panic(fmt.Sprintf("legal: load America/Toronto: %v", err))
	}
	return loc
}

// Flags returns every app/legal flag definition. All of them are off until turned on.
func Flags() []flags.Def {
	return []flags.Def{
		{Name: FlagCardSurcharge, Owner: "accountant/CRA, lawyer/LSO", Expiry: verifyExpiry,
			Doc: "credit-card surcharge on payments: cap, banned cases and HST treatment unconfirmed"},
		{Name: FlagContingencyFees, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "contingency fee agreements: cap formula and LSO standard form unconfirmed"},
		{Name: FlagCreditRouteVerified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "BIL-C-081: credit beyond open AR repaid to trust first, then client credit; By-Law 9 reading unconfirmed"},
		{Name: FlagJointRetainerVerified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "BIL-L-029: joint retainer fees apportioned per payer under r.3.6-4 unconfirmed"},
		{Name: FlagTaxBNCheckVerified, Owner: "accountant/CRA", Expiry: verifyExpiry,
			Doc: "BIL-T-061: GST/HST registration number format and CRA check digit unconfirmed"},
		{Name: FlagReceiptsJournalVerified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "BIL-L-042: receipts journal shows unapplied money as client credit; By-Law 9 record form unconfirmed"},
		{Name: FlagParalegalScopeVerified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "BIL-L-046: paralegal scope-of-practice activity list unconfirmed"},
		{Name: FlagBillingRecordsVerified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "fees book and billing-record retention checks rest on By-Law 9 text not yet confirmed"},
		{Name: FlagSolicitorsActVerified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "Solicitors Act periods and bill and assessment-notice wording unconfirmed"},
		{Name: FlagIDVerification, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "By-Law 7.1 client ID verification exemptions and retention unconfirmed"},
		{Name: FlagDeadlineOntarioPack, Owner: "lawyer/LSO, owner", Expiry: verifyExpiry,
			Doc: "Ontario court and limitation deadline rule pack not yet checked by a lawyer"},
		{Name: FlagExportPostCancelWindow, Owner: "owner", Expiry: verifyExpiry,
			Doc: "90-day export window after cancellation depends on the unconfirmed customer contract"},
		{Name: FlagTaxVerifyRules, Owner: "accountant/CRA", Expiry: verifyExpiry,
			Doc: "HST place of supply for litigation and Indian Act s.87 relief unconfirmed"},
		{Name: FlagTrustBylaw9Verified, Owner: "lawyer/LSO", Expiry: verifyExpiry,
			Doc: "trust accounting rules not yet checked against LSO By-Law 9"},
	}
}

// DefineFlags defines every app/legal flag in s, off.
func DefineFlags(s *flags.Set) error {
	for _, d := range Flags() {
		if err := s.Define(d); err != nil {
			return fmt.Errorf("legal: define flags: %w", err)
		}
	}
	return nil
}
