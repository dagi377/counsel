package boot

import (
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
)

// withWrites fills every nil write port that has a Postgres implementation on the kernel DB:
// the credit-note bill read (POST /credit-notes), the withdrawal service (POST
// /trust/withdrawals), and the reconciliation service and statement balances (prepare and
// submit). None of them approves: the withdrawal and sign-off decisions stay in their gate
// effects. The withdrawal gates default to the route gates when those can withdraw. Ports
// already set are kept. Without a kernel DB nothing is filled.
func (p HTTPPorts) withWrites(k *kboot.Kernel) HTTPPorts {
	if k == nil || k.DB == nil {
		return p
	}
	if p.CreditBills == nil {
		p.CreditBills = credit.PGBills{DB: k.DB}
	}
	if p.WithdrawRequests == nil && k.Clock != nil {
		// Only LockLedger is used, which reads the ledger and its holds as of the business
		// date: no signatory guard or general ledger is needed.
		p.WithdrawRequests = withdraw.PGService{DB: k.DB, Ledgers: trust.NewLedgerStore(k.DB, nil, nil, k.Clock)}
	}
	if g, ok := p.Gates.(withdraw.Gates); ok && p.WithdrawGates == nil {
		p.WithdrawGates = g
	}
	if p.Recons == nil {
		p.Recons = recon.PGService{DB: k.DB}
	}
	if p.ReconStatements == nil {
		p.ReconStatements = recon.TxStatements{DB: k.DB}
	}
	return p
}
