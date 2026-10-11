package boot

import (
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
)

// withReaders fills every nil read port that has a Postgres implementation on the kernel DB:
// credit-note reads (HTTP-09), trust accounts and ledgers (HTTP-10), withdrawals and
// reconciliations (HTTP-11), the gate inbox lister (HTTP-12), the withdrawal ledger reader,
// cancels and gates (HTTP-17), and reconciliation versions and the statement parser
// (HTTP-18). Ports already set are kept. Without a kernel DB nothing is filled.
func (p HTTPPorts) withReaders(k *kboot.Kernel) HTTPPorts {
	if k == nil || k.DB == nil {
		return p
	}
	lists := trust.PGLists{DB: k.DB}
	if p.CreditReads == nil {
		p.CreditReads = credit.PGReads{DB: k.DB}
	}
	if p.TrustLists == nil {
		p.TrustLists = lists
	}
	if p.TrustLedgers == nil && k.Clock != nil {
		p.TrustLedgers = trust.NewLedgerReads(k.DB, k.Clock)
	}
	if p.Withdrawals == nil {
		p.Withdrawals = withdraw.PGLists{DB: k.DB}
	}
	if p.Reconciliations == nil {
		p.Reconciliations = recon.PGLists{DB: k.DB}
	}
	if p.GateList == nil {
		p.GateList = gates.NewPGLister(k.DB)
	}
	if p.WithdrawLedgers == nil {
		p.WithdrawLedgers = withdraw.PGLedgers{Lists: lists}
	}
	if p.WithdrawCancels == nil {
		p.WithdrawCancels = withdraw.PGCancels{DB: k.DB}
	}
	if g, ok := k.Gates.(withdraw.Gates); ok && p.WithdrawGates == nil {
		p.WithdrawGates = g
	}
	if p.ReconVersions == nil {
		p.ReconVersions = recon.PGVersions{DB: k.DB}
	}
	if p.ReconParser == nil {
		// No bank statement parser exists yet: uploads are refused 422, never guessed at.
		p.ReconParser = recon.NoParser{}
	}
	return p
}
