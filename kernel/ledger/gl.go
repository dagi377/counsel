package ledger

import (
	"errors"
	"fmt"
)

// ErrIncompleteGL means NewGL was given no PG ledger or a Balances without a querier.
var ErrIncompleteGL = errors.New("ledger: GL needs a PG and a Balances querier")

// GL is the production Ledger: Post, Reverse and ClosePeriod run on PG, and Balance on
// Balances. Build it with NewGL so neither half is missing.
type GL struct {
	*PG
	Balances
}

var _ Ledger = GL{}

// NewGL joins pg and b into one Ledger. A nil pg or a Balances without a querier is
// ErrIncompleteGL.
func NewGL(pg *PG, b Balances) (GL, error) {
	if pg == nil {
		return GL{}, fmt.Errorf("new gl: no PG: %w", ErrIncompleteGL)
	}
	if b.Q == nil {
		return GL{}, fmt.Errorf("new gl: no Balances querier: %w", ErrIncompleteGL)
	}
	return GL{PG: pg, Balances: b}, nil
}
