package boot

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
)

// withWrites fills every nil write port that has a Postgres implementation on the kernel DB:
// the credit-note bill read (POST /credit-notes), the withdrawal service (POST
// /trust/withdrawals), the reconciliation service and statement balances (prepare and
// submit), and the payment service and proposed allocations (/payments). None of them
// approves: the withdrawal, sign-off and allocation decisions stay in their gate effects.
// The withdrawal gates default to the route gates when those can withdraw. Ports already set
// are kept. Without a kernel DB nothing is filled.
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
		// Gates opens the withdrawal gate in the request tx, so agent withdrawals are gated too.
		svc := withdraw.PGService{DB: k.DB, Ledgers: trust.NewLedgerStore(k.DB, nil, nil, k.Clock)}
		if g, ok := p.Gates.(withdraw.Gates); ok {
			svc.Gates = g
		}
		p.WithdrawRequests = svc
	}
	if g, ok := p.Gates.(withdraw.Gates); ok && p.WithdrawGates == nil {
		p.WithdrawGates = g
	}
	if p.Payments == nil {
		p.Payments = pgPayments(k)
	}
	if p.PaymentProposals == nil {
		p.PaymentProposals = payment.PGProposals{}
	}
	if p.Recons == nil {
		p.Recons = recon.PGService{DB: k.DB}
	}
	if p.ReconStatements == nil {
		p.ReconStatements = recon.TxStatements{DB: k.DB}
	}
	return p
}

// PartyPayers is billing's payment.PayerNames over the party module API: the payer's name is
// the contact's legal name.
type PartyPayers struct {
	Contacts party.Contacts
}

var _ payment.PayerNames = PartyPayers{}

// PayerName implements payment.PayerNames.
func (p PartyPayers) PayerName(ctx context.Context, clientID string) (string, error) {
	c, err := p.Contacts.Get(ctx, party.ContactID(clientID))
	if err != nil {
		return "", fmt.Errorf("payer %s: %w", clientID, err)
	}
	return c.LegalName, nil
}

// pgPayments is the Postgres payment service on k. The HTTP routes and the agent's
// propose_payment_allocation port share it.
func pgPayments(k *kboot.Kernel) payment.PGService {
	// rule: BIL-L-039 (verify)
	// Operating cash shares the trust ledger's per-matter cash limit; without a clock cash fails closed.
	// Billing names the payer only through the party module, never its tables (ARC-I-041).
	svc := payment.PGService{DB: k.DB, Payers: PartyPayers{Contacts: party.NewStore(k.DB)}}
	if k.Clock != nil {
		svc.Cash = trust.NewLedgerStore(k.DB, nil, nil, k.Clock)
	}
	return svc
}
