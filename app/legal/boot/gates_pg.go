package boot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// DefaultBillPrefix is the bill number series prefix when Production sets none.
const DefaultBillPrefix = "INV"

// CapWriteOffSecondApprove is the approver capability of the write-off second approval
// (spec/gates.json billing.writeoff_second_approval).
const CapWriteOffSecondApprove = "billing.writeoff_second_approve"

// managingLawyerTimeout bounds the role lookup of one second-approval check.
const managingLawyerTimeout = 5 * time.Second

// ErrNoPoster means a GL posting found no user to record as posted_by: neither an approver
// of the gate whose effect runs, nor a session user.
var ErrNoPoster = errors.New("legal boot: no posting user")

// PGGL is the production ledger of the gate effects. Post, Reverse and ClosePeriod run on
// ledger.PG inside the caller's tx (integer cents, balanced entries only: ledger.Validate
// and the journal_balanced trigger), and posted_by is resolved in that same tx: the latest
// approver of the gate whose effect runs, else the session user. Balance reads through
// Balances.
type PGGL struct {
	Balances ledger.Balances
}

var _ ledger.Ledger = PGGL{}

const (
	// effectPosterSQL is the latest approver of gate $1, read in the gate tx, where the
	// deciding approval is not yet committed.
	effectPosterSQL = `SELECT decided_by::text FROM gate.gate_decision
 WHERE approval_id = $1::uuid AND decision = 'approve' ORDER BY decided_at DESC, id DESC LIMIT 1`
	sessionPosterSQL = `SELECT coalesce(core.current_user_id()::text, '')`
)

// posterIn returns the posted_by resolver for one tx.
func posterIn(tx pgx.Tx) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		var by string
		if id, ok := gate.InEffect(ctx); ok {
			err := tx.QueryRow(ctx, effectPosterSQL, string(id)).Scan(&by)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return "", fmt.Errorf("posted by: gate %s: %w", id, err)
			}
		}
		if by == "" {
			if err := tx.QueryRow(ctx, sessionPosterSQL).Scan(&by); err != nil {
				return "", fmt.Errorf("posted by: session: %w", err)
			}
		}
		if by == "" {
			return "", ErrNoPoster
		}
		return by, nil
	}
}

// Post implements ledger.Ledger.
//
// rule: BIL-I-001
func (g PGGL) Post(ctx context.Context, tx pgx.Tx, e ledger.Entry) (ledger.EntryID, error) {
	if err := ledger.Validate(e); err != nil {
		return "", fmt.Errorf("gl post: %w", err)
	}
	return (&ledger.PG{PostedBy: posterIn(tx)}).Post(ctx, tx, e)
}

// Reverse implements ledger.Ledger.
func (g PGGL) Reverse(ctx context.Context, tx pgx.Tx, id ledger.EntryID, reason string) (ledger.EntryID, error) {
	return (&ledger.PG{PostedBy: posterIn(tx)}).Reverse(ctx, tx, id, reason)
}

// ClosePeriod implements ledger.Ledger.
func (g PGGL) ClosePeriod(ctx context.Context, tx pgx.Tx, book string, p ledger.Period) error {
	return (&ledger.PG{PostedBy: posterIn(tx)}).ClosePeriod(ctx, tx, book, p)
}

// Balance implements ledger.Ledger.
func (g PGGL) Balance(ctx context.Context, account string, asOf ledger.Date) (ledger.Cents, error) {
	if g.Balances.Q == nil {
		return 0, fmt.Errorf("gl balance: %w", ErrMissingDependency)
	}
	return g.Balances.Balance(ctx, account, asOf)
}

// managingLawyer is credit.SecondApproval.ManagingLawyer over the capability checker: a user
// qualifies when one of their roles holds CapWriteOffSecondApprove. Any lookup error fails
// closed.
//
// rule: BIL-C-085
func managingLawyer(access Access) func(userID string) bool {
	return func(userID string) bool {
		if access == nil || userID == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), managingLawyerTimeout)
		defer cancel()
		ok, err := access.Can(ctx, registry.Principal{Kind: registry.PrincipalUser, UserID: userID},
			CapWriteOffSecondApprove, registry.Subject{})
		return err == nil && ok
	}
}

// gatePortsPG builds the Postgres GatePorts on the kernel: the GL (PGGL), the account-number
// Sealer (AES-256-GCM with seal), the withdrawal Auditor (kernel audit log), the signatory,
// signer and firm ports, the billing effects, and the write-off ports with the managing
// lawyer's second approval resolved through access.
func gatePortsPG(k *kboot.Kernel, conn pgConn, seal trust.SealKey, prefix string, log audit.Log, access Access) (*GatePorts, error) {
	if k == nil || k.DB == nil || k.Gates == nil {
		return nil, fmt.Errorf("gate ports: kernel DB and Gates: %w", ErrMissingDependency)
	}
	if log == nil || access == nil {
		return nil, fmt.Errorf("gate ports: audit log and access: %w", ErrMissingDependency)
	}
	sealer, err := trust.NewAESSealer(seal)
	if err != nil {
		return nil, fmt.Errorf("gate ports: %w", err)
	}
	if prefix == "" {
		prefix = DefaultBillPrefix
	}
	gl := PGGL{Balances: ledger.Balances{Q: conn}}
	matters := matter.NewStore(k.DB, party.NewStore(k.DB))
	return &GatePorts{
		Trust: TrustPorts{GL: gl, Seal: sealer, Audit: withdraw.NewAuditLog(log)}.WithPostgres(k.DB),
		Billing: BillingPorts{
			GL:     gl,
			Prefix: prefix,
		},
		Credit: CreditPorts{
			Gates:  k.Gates,
			Second: credit.SecondApproval{ManagingLawyer: managingLawyer(access)},
		}.WithPostgres(gl, matters),
	}, nil
}
