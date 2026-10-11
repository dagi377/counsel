// CONTRACT: kernel/numseq types and interfaces. Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html
// (numseq.Next(ctx, tx, series string) (string, error)) and table core.number_series.

package numseq

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Series is an app-defined number series. Format tokens: {YYYY} is the business year of the
// issue date; {SEQ:N} is the counter zero-padded to N digits. A format with {YYYY} restarts
// the counter at 1 each business year. Example: "{YYYY}-{SEQ:4}" -> "2026-0001".
type Series struct {
	Name   string
	Format string
}

// Sequencer issues gapless numbers. Next runs in the caller's transaction and locks the
// series row (SELECT FOR UPDATE), so a rolled-back transaction does not consume a number.
type Sequencer interface {
	// Define registers a series at boot. Redefining a name with a different format fails.
	Define(ctx context.Context, s Series) error
	// Next returns the next formatted number of series inside tx.
	Next(ctx context.Context, tx pgx.Tx, series string) (string, error)
}

// ErrUnknownSeries is returned by Next for a series that was never defined.
var ErrUnknownSeries = errors.New("numseq: unknown series")

// ErrBadFormat is returned by Define when the format has no {SEQ:N} token or an unknown token.
var ErrBadFormat = errors.New("numseq: bad series format")

// ErrConflict is returned by Define when the name exists with a different format.
var ErrConflict = errors.New("numseq: series already defined with another format")
