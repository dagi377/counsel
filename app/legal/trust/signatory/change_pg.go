package signatory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
)

// Change ops as trust.signatory_change stores them.
const (
	opAdd    = "add"
	opRemove = "remove"
)

// insertChangeSQL records a pending change.
const insertChangeSQL = `INSERT INTO trust.signatory_change (account_id, user_id, op, kind, bond_evidence, proposed_by)
VALUES ($1::uuid, $2::uuid, $3, NULLIF($4, ''), NULLIF($5, ''), $6::uuid)
RETURNING id::text`

// lockChangeSQL reads one change FOR UPDATE.
const lockChangeSQL = `SELECT id::text, account_id::text, user_id::text, op, COALESCE(kind, ''), COALESCE(bond_evidence, ''),
  proposed_by::text, COALESCE(approved_by::text, ''), approved_at, status
FROM trust.signatory_change WHERE id = $1::uuid FOR UPDATE`

// lockSignatoriesSQL locks the account's current grants, so two changes on one account are
// decided one after the other and the last-signatory check holds.
const lockSignatoriesSQL = signatorySQL + ` FOR UPDATE`

// insertSignatorySQL grants authority from today. A bonded non-licensee carries a bond row
// (bond_cents 0, no expiry) so the table CHECK holds; the evidence stays on the change.
const insertSignatorySQL = `INSERT INTO trust.signatory (account_id, user_id, is_licensee, bond_cents, valid_from, approval_id)
VALUES ($1::uuid, $2::uuid, $3, $4, $5::date, $6::uuid)`

// endSignatorySQL ends the user's open grant on the account today.
const endSignatorySQL = `UPDATE trust.signatory SET valid_to = GREATEST(valid_from, $3::date)
WHERE account_id = $1::uuid AND user_id = $2::uuid AND valid_to IS NULL`

// markApprovedSQL marks a pending change approved.
const markApprovedSQL = `UPDATE trust.signatory_change
SET status = 'approved', approved_by = $2::uuid, approval_id = $3::uuid, approved_at = $4
WHERE id = $1::uuid AND status = 'pending'`

// changeRow is one trust.signatory_change row.
type changeRow struct {
	ID, AccountID, UserID, Op, Kind, BondEvidence, ProposedBy, ApprovedBy string
	ApprovedAt                                                            *time.Time
	Status                                                                string
}

// change maps r to a Change.
func (r changeRow) change() Change {
	c := Change{ID: r.ID, AccountID: r.AccountID, PreparedBy: r.ProposedBy, Status: ChangeStatus(r.Status),
		ApprovedBy: r.ApprovedBy, ApprovedAt: r.ApprovedAt}
	if r.Op == opAdd {
		c.Add = []Signatory{{AccountID: r.AccountID, UserID: r.UserID, Kind: Kind(r.Kind), BondEvidence: r.BondEvidence}}
	} else {
		c.Remove = []string{r.UserID}
	}
	return c
}

// approvalKey carries the approving gate's id from TxEffects to Approve.
type approvalKey struct{}

// txDB runs every InTx on one caller transaction: the gate tx an effect runs in.
type txDB struct {
	db.DB
	tx pgx.Tx
}

func (d txDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

// OnTx returns a PGService that runs every read and write on tx instead of its own
// transactions; it commits nothing itself.
func (s *PGService) OnTx(tx pgx.Tx) *PGService {
	return &PGService{db: txDB{DB: s.db, tx: tx}}
}

// TxEffects is Effects over s bound to the gate tx: the change is proposed and approved in
// the same transaction as the gate decision, and trust.signatory.approval_id is the gate id.
func TxEffects(s *PGService) map[string]registry.EffectFunc {
	out := map[string]registry.EffectFunc{}
	for kind := range Effects(s) {
		out[kind] = func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
			ctx = context.WithValue(ctx, approvalKey{}, g.GateID)
			return Effects(s.OnTx(tx))[kind](ctx, tx, g)
		}
	}
	return out
}

// Propose implements Service: it checks the one signatory change in c and records it
// pending. A change adds or removes exactly one user.
func (s *PGService) Propose(ctx context.Context, c Change) (Change, error) {
	if c.AccountID == "" || c.PreparedBy == "" || len(c.Add)+len(c.Remove) != 1 {
		return Change{}, fmt.Errorf("propose signatory change: one add or remove: %w", ErrInvalidState)
	}
	r := changeRow{AccountID: c.AccountID, ProposedBy: c.PreparedBy, Status: string(ChangePending)}
	if len(c.Add) == 1 {
		a := c.Add[0]
		r.Op, r.UserID, r.Kind, r.BondEvidence = opAdd, a.UserID, string(a.Kind), a.BondEvidence
	} else {
		r.Op, r.UserID = opRemove, c.Remove[0]
	}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.check(ctx, tx, r); err != nil {
			return err
		}
		return tx.QueryRow(ctx, insertChangeSQL, r.AccountID, r.UserID, r.Op, r.Kind, r.BondEvidence, r.ProposedBy).Scan(&r.ID)
	})
	if malformed(err) {
		return Change{}, fmt.Errorf("propose signatory change: %w", ErrNotFound)
	}
	if err != nil {
		return Change{}, fmt.Errorf("propose signatory change: %w", err)
	}
	return r.change(), nil
}

// check validates r against the directory and the account's current grants: an added user
// is active and a licensee, or bonded with evidence; a removal never leaves the account with
// no one who can sign.
func (s *PGService) check(ctx context.Context, tx pgx.Tx, r changeRow) error {
	if r.Op == opAdd {
		u, err := NewPGDirectory(txDB{DB: s.db, tx: tx}).User(ctx, r.UserID)
		if errors.Is(err, ErrNotFound) {
			return ErrNotEligible
		}
		if err != nil {
			return err
		}
		switch {
		case !u.Active:
			return ErrNotEligible
		case Kind(r.Kind) == Licensee && !u.Licensee:
			return ErrNotEligible
		case Kind(r.Kind) == BondedNonLicensee && r.BondEvidence == "":
			return ErrNotEligible
		case Kind(r.Kind) != Licensee && Kind(r.Kind) != BondedNonLicensee:
			return ErrNotEligible
		}
		return nil
	}
	today := civil(clock.Today(ctx))
	rs, err := scanRows(ctx, tx, lockSignatoriesSQL, r.AccountID, today)
	if err != nil {
		return err
	}
	for _, x := range rs {
		if x.UserID != r.UserID && x.canSign(today) {
			return nil
		}
	}
	return ErrLastSignatory
}

// Approve implements Service: a licensee other than the proposer approves a pending change,
// which then applies to trust.signatory with the approval id (the gate id under TxEffects,
// else the change id). Approving an approved change again changes nothing.
func (s *PGService) Approve(ctx context.Context, id string, by Approver) (Change, error) {
	var out Change
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var r changeRow
		err := tx.QueryRow(ctx, lockChangeSQL, id).Scan(&r.ID, &r.AccountID, &r.UserID, &r.Op, &r.Kind, &r.BondEvidence,
			&r.ProposedBy, &r.ApprovedBy, &r.ApprovedAt, &r.Status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if r.Status == string(ChangeApproved) {
			out = r.change()
			return nil
		}
		if by.IsAgent {
			return ErrAgentApproval
		}
		if by.UserID == "" || by.UserID == r.ProposedBy {
			return ErrSelfApproval
		}
		u, err := NewPGDirectory(txDB{DB: s.db, tx: tx}).User(ctx, by.UserID)
		if errors.Is(err, ErrNotFound) || (err == nil && (!u.Licensee || !u.Active)) {
			return ErrNotEligible
		}
		if err != nil {
			return err
		}
		// Re-check eligibility and the last signatory under the account lock.
		if err := s.check(ctx, tx, r); err != nil {
			return err
		}
		approval, _ := ctx.Value(approvalKey{}).(string)
		if approval == "" {
			approval = r.ID
		}
		today := civil(clock.Today(ctx))
		if r.Op == opAdd {
			var bond *int64
			if Kind(r.Kind) == BondedNonLicensee {
				zero := int64(0)
				bond = &zero
			}
			if _, err := tx.Exec(ctx, insertSignatorySQL, r.AccountID, r.UserID, Kind(r.Kind) == Licensee, bond, today, approval); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, endSignatorySQL, r.AccountID, r.UserID, today); err != nil {
			return err
		}
		clk, err := clock.From(ctx)
		if err != nil {
			return err
		}
		at := clk.Now()
		if _, err := tx.Exec(ctx, markApprovedSQL, r.ID, by.UserID, approval, at); err != nil {
			return err
		}
		r.Status, r.ApprovedBy, r.ApprovedAt = string(ChangeApproved), by.UserID, &at
		out = r.change()
		return nil
	})
	if malformed(err) {
		return Change{}, fmt.Errorf("approve signatory change: %w", ErrNotFound)
	}
	if err != nil {
		return Change{}, fmt.Errorf("approve signatory change: %w", err)
	}
	return out, nil
}

// scanRows reads trust.signatory rows with sql in tx.
func scanRows(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]row, error) {
	rs, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.UserID, &r.Licensee, &r.Bonded, &r.BondExpiry, &r.ValidFrom, &r.ValidTo); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}
