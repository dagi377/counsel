package withdraw

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// ErrNoFirm means core.firm has no row, so the firm's sole-practitioner setting is unknown.
var ErrNoFirm = errors.New("withdraw: firm settings not found")

// PGFirm reads the firm's sole-practitioner setting from core.firm, the singleton firm row.
// It only reads, in its own transaction. It fails closed: a missing row or a read error is
// returned, never defaulted, so the withdrawal effect never assumes a sole practitioner.
type PGFirm struct {
	db db.DB
}

var _ FirmPolicy = (*PGFirm)(nil)

// NewPGFirm returns a PGFirm on pool.
func NewPGFirm(pool db.DB) *PGFirm {
	return &PGFirm{db: pool}
}

const firmSoleSQL = `SELECT f.sole_practitioner FROM core.firm f WHERE f.singleton`

// SolePractitioner implements FirmPolicy.
func (f *PGFirm) SolePractitioner(ctx context.Context) (bool, error) {
	var solo bool
	err := f.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, firmSoleSQL).Scan(&solo)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("firm sole practitioner: %w", ErrNoFirm)
	}
	if err != nil {
		return false, fmt.Errorf("firm sole practitioner: %w", err)
	}
	return solo, nil
}
