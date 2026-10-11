package recon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust/recon"
)

// closingReader is the recon.Statements port (HTTP-18b); PGStatements must satisfy it.
type closingReader interface {
	Closing(ctx context.Context, accountID string, p recon.Period) (int64, error)
}

var _ closingReader = (*recon.PGStatements)(nil)

const stmtAccount = "0190a000-0000-7000-8000-0000000000a1"

// closingRow answers Scan with closing, or err.
type closingRow struct {
	closing int64
	err     error
}

func (r closingRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*int64) = r.closing
	return nil
}

// closingQuerier records the query arguments and answers with row.
type closingQuerier struct {
	row   closingRow
	calls int
	args  []any
}

func (q *closingQuerier) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.calls++
	q.args = args
	return q.row
}

func TestPGStatementsClosing(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		account   string
		period    recon.Period
		row       closingRow
		want      int64
		wantErr   error
		wantCalls int
		wantDay   time.Time
	}{
		{name: "closing in cents", account: stmtAccount, period: recon.Period{Year: 2026, Month: time.March}, row: closingRow{closing: 1234567},
			want: 1234567, wantCalls: 1, wantDay: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)},
		{name: "negative closing kept", account: stmtAccount, period: recon.Period{Year: 2026, Month: time.February}, row: closingRow{closing: -50},
			want: -50, wantCalls: 1, wantDay: time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)},
		{name: "december rolls the year", account: stmtAccount, period: recon.Period{Year: 2026, Month: time.December}, row: closingRow{closing: 1},
			want: 1, wantCalls: 1, wantDay: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)},
		{name: "no statement", account: stmtAccount, period: recon.Period{Year: 2026, Month: time.March}, row: closingRow{err: pgx.ErrNoRows},
			wantErr: recon.ErrNotFound, wantCalls: 1, wantDay: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)},
		{name: "db error", account: stmtAccount, period: recon.Period{Year: 2026, Month: time.March}, row: closingRow{err: boom},
			wantErr: boom, wantCalls: 1, wantDay: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)},
		{name: "malformed account", account: "acct-1", period: recon.Period{Year: 2026, Month: time.March}, wantErr: recon.ErrNotFound},
		{name: "bad month", account: stmtAccount, period: recon.Period{Year: 2026, Month: 13}, wantErr: recon.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &closingQuerier{row: tt.row}
			got, err := recon.NewPGStatements(q).Closing(context.Background(), tt.account, tt.period)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("Closing = %d, %v; want %d, %v", got, err, tt.want, tt.wantErr)
			}
			if q.calls != tt.wantCalls {
				t.Fatalf("queries = %d, want %d", q.calls, tt.wantCalls)
			}
			if tt.wantCalls == 1 && (q.args[0] != tt.account || !q.args[1].(time.Time).Equal(tt.wantDay)) {
				t.Fatalf("args = %v, want %s %s", q.args, tt.account, tt.wantDay)
			}
		})
	}
}
