package ledger

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// sqlLineDimensions lists the gl.journal_line columns beyond the core ones, with their SQL
// types, in table order. Apps add their own link columns; the kernel never names them.
const sqlLineDimensions = `SELECT a.attname, format_type(a.atttypid, a.atttypmod)
FROM pg_attribute a
WHERE a.attrelid = 'gl.journal_line'::regclass AND a.attnum > 0 AND NOT a.attisdropped
  AND a.attname NOT IN ('id', 'entry_id', 'account_id', 'debit_cents', 'credit_cents')
ORDER BY a.attnum`

// dimension is one optional gl.journal_line column.
type dimension struct {
	Name string // column name
	Type string // SQL type as format_type prints it, e.g. "uuid" or "text"
}

func (d dimension) ident() string { return pgx.Identifier{d.Name}.Sanitize() }

// lineDimensions reads the optional journal_line columns from the catalog.
func lineDimensions(ctx context.Context, tx pgx.Tx) ([]dimension, error) {
	q, err := tx.Query(ctx, sqlLineDimensions)
	if err != nil {
		return nil, fmt.Errorf("line dimensions: %w", err)
	}
	defer q.Close()
	var out []dimension
	for q.Next() {
		var d dimension
		if err := q.Scan(&d.Name, &d.Type); err != nil {
			return nil, fmt.Errorf("line dimensions: scan: %w", err)
		}
		out = append(out, d)
	}
	if err := q.Err(); err != nil {
		return nil, fmt.Errorf("line dimensions: %w", err)
	}
	return out, nil
}

// checkDimensions returns ErrInvalidEntry when a line names a dimension the table lacks.
func checkDimensions(lines []Line, dims []dimension) error {
	for i, l := range lines {
		for k := range l.Dimensions {
			if !slices.ContainsFunc(dims, func(d dimension) bool { return d.Name == k }) {
				return fmt.Errorf("line %d dimension %q: %w", i, k, ErrInvalidEntry)
			}
		}
	}
	return nil
}

// insertLineSQL is the journal_line insert for the core columns plus dims. Parameters are
// $1 entry, $2 account, $3 debit, $4 credit, then one text value per dim, cast to its type.
func insertLineSQL(dims []dimension) string {
	cols := []string{"entry_id", "account_id", "debit_cents", "credit_cents"}
	vals := []string{"$1::uuid", "$2::uuid", "$3", "$4"}
	for i, d := range dims {
		cols = append(cols, d.ident())
		vals = append(vals, fmt.Sprintf("$%d::text::%s", i+5, d.Type))
	}
	return "INSERT INTO gl.journal_line (" + strings.Join(cols, ", ") + ")\nVALUES (" + strings.Join(vals, ", ") + ")"
}

// selectLinesSQL reads an entry's lines: account, debit, credit, then every dim as text.
func selectLinesSQL(dims []dimension) string {
	cols := []string{"account_id::text", "debit_cents", "credit_cents"}
	for _, d := range dims {
		cols = append(cols, d.ident()+"::text")
	}
	return "SELECT " + strings.Join(cols, ", ") + "\nFROM gl.journal_line WHERE entry_id = $1::uuid ORDER BY id"
}

// dimArgs returns the dims' values in dims order: nil (NULL) for an absent or empty value.
func dimArgs(vals map[string]*string, dims []dimension) []any {
	out := make([]any, len(dims))
	for i, d := range dims {
		if v := vals[d.Name]; v != nil {
			out[i] = *v
		}
	}
	return out
}

// lineDims converts a Line's dimension map to the nullable form rows use.
func lineDims(m map[string]string) map[string]*string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]*string, len(m))
	for k, v := range m {
		if v != "" {
			out[k] = &v
		}
	}
	return out
}
