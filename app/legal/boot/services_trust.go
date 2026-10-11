package boot

import (
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/ledger"
)

// ErrMissingDependency is returned when a service is built without a dependency it needs.
// The error text names the dependency.
var ErrMissingDependency = errors.New("legal boot: missing dependency")

// TrustPorts are the trust services' dependencies that other modules own. No production
// implementation of most of them exists yet (Q-BOOT-13-PORTS), so the composition root takes
// them from its caller.
type TrustPorts struct {
	// GL is the kernel ledger. The trust LedgerStore posts on it with book trust and source
	// kind trust (Q-GL-BOOK), so the trust book needs no ledger of its own.
	GL ledger.Ledger
	// Users reads identity users for signatory eligibility.
	Users signatory.Directory
	// Seal encrypts account numbers before they are stored.
	Seal trust.Sealer
	// Signatories manages signing authority (the core.trust_signatory effect's service).
	Signatories signatory.Service
	// Signers answers the withdrawal effect's signatory check inside the gate tx.
	Signers withdraw.SignatoryCheck
	// Firm reads core.firm.sole_practitioner.
	Firm withdraw.FirmPolicy
	// Audit records withdrawal audit actions.
	Audit withdraw.Auditor
	// FeeBills reads the bill a fee withdrawal pays (WD-BILL). Nil uses PGFeeBills.
	FeeBills withdraw.FeeBills
}

// trustSvcs are the trust services built at boot.
type trustSvcs struct {
	Accounts    *trust.AccountStore
	Ledgers     *trust.LedgerStore
	Signatories signatory.Service
	Withdraw    *withdraw.Effect
	// Transfer is the trust.transfer effect: it authorizes a transfer row and posts its two
	// trust lines for a signatory other than the preparer.
	Transfer *withdraw.TransferEffect
}

// trustServices builds the trust services from module constructors. The withdrawal effect
// moves money through the same LedgerStore that posts receipts, so both see one trust book.
func trustServices(k *kboot.Kernel, p TrustPorts) (trustSvcs, error) {
	if k == nil {
		return trustSvcs{}, fmt.Errorf("trust services: kernel: %w", ErrMissingDependency)
	}
	for _, d := range []struct {
		name string
		nil  bool
	}{
		{"kernel DB", k.DB == nil},
		{"kernel Clock", k.Clock == nil},
		{"GL", p.GL == nil},
		{"Users", p.Users == nil},
		{"Seal", p.Seal == nil},
		{"Signatories", p.Signatories == nil},
		{"Signers", p.Signers == nil},
		{"Firm", p.Firm == nil},
		{"Audit", p.Audit == nil},
	} {
		if d.nil {
			return trustSvcs{}, fmt.Errorf("trust services: %s: %w", d.name, ErrMissingDependency)
		}
	}
	accounts := trust.NewAccountStore(k.DB, p.Users, p.Seal)
	ledgers := trust.NewLedgerStore(k.DB, accounts, p.GL, k.Clock)
	return trustSvcs{
		Accounts:    accounts,
		Ledgers:     ledgers,
		Signatories: p.Signatories,
		Withdraw: &withdraw.Effect{
			Ledgers: ledgers,
			Store:   withdraw.PGStore{},
			Signers: p.Signers,
			Firm:    p.Firm,
			Audit:   p.Audit,
			Bills:   feeBillsOrPG(p.FeeBills),
			Now:     k.Clock.Now,
		},
		Transfer: &withdraw.TransferEffect{
			Store:     withdraw.PGTransferStore{},
			Transfers: &withdraw.Transfers{Store: withdraw.PGTransferStore{}, GL: p.GL, Loc: k.Clock.Location()},
			Signers:   p.Signers,
			Now:       k.Clock.Now,
		},
	}, nil
}

// feeBillsOrPG is b, or the Postgres bill reader when b is nil, so a fee withdrawal on a
// delivered bill can be authorised.
func feeBillsOrPG(b withdraw.FeeBills) withdraw.FeeBills {
	if b == nil {
		return PGFeeBills{}
	}
	return b
}
