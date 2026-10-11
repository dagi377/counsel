package withdraw

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// firmRow is the scripted core.firm row: sole_practitioner, or err.
type firmRow struct {
	solo bool
	err  error
}

func (r firmRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.solo
	return nil
}

// firmTx answers every QueryRow with row and records the SQL it saw.
type firmTx struct {
	pgx.Tx
	row firmRow
	sql []string
}

func (t *firmTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.sql = append(t.sql, sql)
	return t.row
}

// firmDB runs fn on tx, or fails to begin with beginErr, like a real InTx.
type firmDB struct {
	db.DB
	tx       *firmTx
	beginErr error
}

func (d firmDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if d.beginErr != nil {
		return d.beginErr
	}
	return fn(ctx, d.tx)
}

func TestPGFirmSolePractitioner(t *testing.T) {
	boom := errors.New("connection reset")
	tests := []struct {
		name     string
		row      firmRow
		beginErr error
		want     bool
		err      error
	}{
		{name: "sole practitioner", row: firmRow{solo: true}, want: true},
		{name: "not sole practitioner", row: firmRow{solo: false}, want: false},
		{name: "no firm row fails closed", row: firmRow{err: pgx.ErrNoRows}, err: ErrNoFirm},
		{name: "read error wrapped", row: firmRow{err: boom}, err: boom},
		{name: "begin error wrapped", beginErr: db.ErrClosed, err: db.ErrClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &firmTx{row: tt.row}
			got, err := NewPGFirm(firmDB{tx: tx, beginErr: tt.beginErr}).SolePractitioner(context.Background())
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "firm sole practitioner: ") {
				t.Fatalf("err = %q, want it wrapped with context", err)
			}
			// On any error the answer is false: the effect must not assume a sole practitioner.
			if got != tt.want {
				t.Fatalf("solo = %v, want %v", got, tt.want)
			}
			if tt.beginErr == nil && (len(tx.sql) != 1 || !strings.Contains(tx.sql[0], "FROM core.firm")) {
				t.Fatalf("sql = %q", tx.sql)
			}
		})
	}
}
