package gate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ListFilter selects one page of gates, ordered by id.
type ListFilter struct {
	// After keeps gates whose id sorts after it; empty starts at the first.
	After GateID
	// Limit is the page size; it is bounded to 1..MaxListLimit (0 means DefaultListLimit).
	Limit int
	// States keeps gates in one of these states; empty keeps all.
	States []Status
	// Kinds keeps gates of one of these kinds; nil keeps all, empty keeps none.
	Kinds []string
	// NotPreparedBy drops gates this user prepared, including those an agent prepared while
	// acting for them.
	NotPreparedBy string
}

// Page sizes PGList uses when ListFilter.Limit is out of range.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// PGList lists gates over the gate tables. Each page runs in one transaction on DB, so reads
// run under whatever session the runner binds.
type PGList struct {
	DB TxRunner
}

// listGatesSQL selects one page of gate ids. Empty filters are NULL, so the uuid casts never
// see an empty string. requested_by is the preparing user, or the user an agent acted for.
const listGatesSQL = `SELECT id::text FROM gate.approval_request
WHERE (NULLIF($1, '')::uuid IS NULL OR id > NULLIF($1, '')::uuid)
  AND (cardinality($2::text[]) = 0 OR status = ANY($2::text[]))
  AND ($3::text[] IS NULL OR gate_code = ANY($3::text[]))
  AND (NULLIF($4, '')::uuid IS NULL OR requested_by <> NULLIF($4, '')::uuid)
ORDER BY id LIMIT $5`

// List returns one page of gates, each read as Get reads it.
func (l PGList) List(ctx context.Context, f ListFilter) ([]Gate, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("list gates: %w", ErrNoDB)
	}
	if f.Kinds != nil && len(f.Kinds) == 0 {
		return nil, nil
	}
	states := make([]string, 0, len(f.States))
	for _, s := range f.States {
		states = append(states, string(s))
	}
	var out []Gate
	err := l.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ids, err := pageIDs(ctx, tx, f, states)
		if err != nil {
			return err
		}
		for _, id := range ids {
			g, err := getGate(ctx, tx, GateID(id))
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list gates: %w", err)
	}
	return out, nil
}

func pageIDs(ctx context.Context, tx pgx.Tx, f ListFilter, states []string) ([]string, error) {
	rows, err := tx.Query(ctx, listGatesSQL, string(f.After), states, f.Kinds, f.NotPreparedBy, listLimit(f.Limit))
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return ids, nil
}

func listLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultListLimit
	case n > MaxListLimit:
		return MaxListLimit
	}
	return n
}
