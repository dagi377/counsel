package recon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/registry"
)

// SignOffState is a trust.reconciliation row as the sign-off effect reads it, locked in the
// gate tx.
type SignOffState struct {
	PreparedBy string
	Status     string
	ApprovalID string // the gate the row was signed under, "" before
	// The three-way figures the approval binds, in cents.
	AdjustedBankCents       int64
	JournalBalanceCents     int64
	ClientListingTotalCents int64
	// PackageSHA256 is the SHA-256 of the reconciliation package sealed at sign-off: the
	// month, the balances, the reconciling items and the client listing snapshot.
	PackageSHA256 []byte
}

// SignOffStore keeps reconciliation sign-offs for the gate effect. Every method runs in the
// gate tx. PGSignOff implements it.
type SignOffStore interface {
	// LoadForSignOff locks reconciliation id and returns its state, or ErrNotFound.
	LoadForSignOff(ctx context.Context, tx pgx.Tx, id string) (SignOffState, error)
	// Sign records the sign-off (PGSignOff.Sign).
	Sign(ctx context.Context, tx pgx.Tx, r SignOffRecord) error
}

// SignOffPayload is the trust.reconciliation_signoff gate payload the sign-off route opens:
// the figures it binds are checked against the row before it is signed.
type SignOffPayload struct {
	ReconciliationID        string `json:"reconciliation_id"`
	AdjustedBankCents       *int64 `json:"adjusted_bank_cents"`
	JournalBalanceCents     *int64 `json:"journal_balance_cents"`
	ClientListingTotalCents *int64 `json:"client_listing_total_cents"`
}

// SignOffEffect is the trust.reconciliation_signoff gate effect: in the gate's tx it signs the
// reconciliation the gate was opened on, which locks its month (By-law 9 s.18). It is the only
// path that signs a reconciliation (ErrEffectOnly).
type SignOffEffect struct {
	Store SignOffStore
	Now   func() time.Time
}

// SignOffEffects returns the effect handler keyed by gate kind.
func SignOffEffects(e *SignOffEffect) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{GateSignOff: e.Apply}
}

// Apply is the trust.reconciliation_signoff EffectFunc. It fails closed while the By-law 9
// flag is off, on an agent approver, on the gate's preparer (unless they also prepared the row,
// where Sign applies the audited sole-practitioner exception), and on approved figures that
// differ from the row. A row this gate already signed is a no-op.
func (e *SignOffEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	// rule: BIL-I-025
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return fmt.Errorf("sign-off effect (gate %s): %w", g.GateID, ErrUnverified)
	}
	if e == nil || e.Store == nil || e.Now == nil {
		return fmt.Errorf("sign-off effect (gate %s): not wired: %w", g.GateID, ErrInvalidState)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("sign-off effect (gate %s): no approver: %w", g.GateID, ErrInvalidState)
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return fmt.Errorf("sign-off effect (gate %s): %w", g.GateID, ErrAgentApproval)
		}
	}
	var p SignOffPayload
	if len(g.Payload) > 0 {
		if err := json.Unmarshal(g.Payload, &p); err != nil {
			return fmt.Errorf("sign-off effect (gate %s): payload: %w", g.GateID, ErrInvalidState)
		}
	}
	id := g.Subject.ID
	if id == "" || (p.ReconciliationID != "" && p.ReconciliationID != id) {
		return fmt.Errorf("sign-off effect (gate %s): reconciliation: %w", g.GateID, ErrInvalidState)
	}
	st, err := e.Store.LoadForSignOff(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("sign-off effect (gate %s): %w", g.GateID, err)
	}
	signer := g.Approvers[len(g.Approvers)-1]
	gatePrep := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		gatePrep = g.Preparer.ActingFor
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-021
		// The person who opened the sign-off never signs it; the row's own preparer reaches
		// Sign, which allows only the audited sole-practitioner exception.
		if gatePrep != "" && a.UserID == gatePrep && a.UserID != st.PreparedBy {
			return fmt.Errorf("sign-off effect (gate %s): %w", g.GateID, ErrSelfApproval)
		}
	}
	// rule: ARC-I-012
	// Idempotent per gate: a row this gate already signed is left as it is.
	if st.Status == "signed" && st.ApprovalID == g.GateID {
		return nil
	}
	// rule: BIL-I-009
	// The approval binds the figures it was shown: a row whose balances moved since is not signed.
	for _, f := range []struct {
		approved *int64
		row      int64
	}{
		{p.AdjustedBankCents, st.AdjustedBankCents},
		{p.JournalBalanceCents, st.JournalBalanceCents},
		{p.ClientListingTotalCents, st.ClientListingTotalCents},
	} {
		if f.approved != nil && *f.approved != f.row {
			return fmt.Errorf("sign-off effect (gate %s): approved figures differ: %w", g.GateID, ErrUnbalanced)
		}
	}
	return e.Store.Sign(ctx, tx, SignOffRecord{
		ReconciliationID: id,
		By:               Approver{UserID: signer.UserID},
		At:               e.Now(),
		PackageSHA256:    st.PackageSHA256,
		ApprovalID:       g.GateID,
	})
}

// loadForSignOffSQL locks the row and seals its package: SHA-256 over the canonical JSON of
// the month, balances, reconciling items and the client listing snapshot.
const loadForSignOffSQL = `SELECT prepared_by::text, status, coalesce(approval_id::text, ''),
  adjusted_bank_cents, journal_balance_cents, client_listing_total_cents,
  sha256(convert_to(jsonb_build_object(
    'id', id, 'account_id', account_id, 'month', month,
    'bank_balance_cents', bank_balance_cents, 'adjusted_bank_cents', adjusted_bank_cents,
    'journal_balance_cents', journal_balance_cents, 'client_listing_total_cents', client_listing_total_cents,
    'outstanding_items', outstanding_items, 'client_listing_snapshot', client_listing_snapshot)::text, 'UTF8'))
FROM trust.reconciliation WHERE id = $1::uuid FOR UPDATE`

// LoadForSignOff implements SignOffStore.
func (s *PGSignOff) LoadForSignOff(ctx context.Context, tx pgx.Tx, id string) (SignOffState, error) {
	var st SignOffState
	err := tx.QueryRow(ctx, loadForSignOffSQL, id).Scan(&st.PreparedBy, &st.Status, &st.ApprovalID,
		&st.AdjustedBankCents, &st.JournalBalanceCents, &st.ClientListingTotalCents, &st.PackageSHA256)
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return SignOffState{}, fmt.Errorf("recon load %s: %w", id, ErrNotFound)
		}
		return SignOffState{}, fmt.Errorf("recon load %s: %w", id, err)
	}
	return st, nil
}

var _ SignOffStore = (*PGSignOff)(nil)
