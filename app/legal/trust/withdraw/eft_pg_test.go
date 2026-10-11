package withdraw

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// eftBinding is one trust.withdrawal_request row bound to the record.
type eftBinding struct {
	withdrawalID string
	amountCents  int64
}

// eftRows yields bindings as (id, amount_cents) rows, then err from Err.
type eftRows struct {
	pgx.Rows
	vals []eftBinding
	i    int
	err  error
}

func (r *eftRows) Next() bool { r.i++; return r.i <= len(r.vals) }
func (r *eftRows) Scan(dest ...any) error {
	b := r.vals[r.i-1]
	*dest[0].(*string) = b.withdrawalID
	*dest[1].(*int64) = b.amountCents
	return nil
}
func (r *eftRows) Err() error { return r.err }
func (r *eftRows) Close()     {}

// eftDocRow is the docs.document row: who filed the record and when, or err.
type eftDocRow struct {
	by  string
	at  time.Time
	err error
}

func (r eftDocRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.by
	*dest[1].(*time.Time) = r.at
	return nil
}

// eftTx is a fake DB: the binding query returns bindings (or queryErr, or rowsErr from Err),
// and the document query returns doc. It records the SQL it saw.
type eftTx struct {
	pgx.Tx
	bindings []eftBinding
	queryErr error
	rowsErr  error
	doc      eftDocRow
	sql      []string
}

func (t *eftTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	t.sql = append(t.sql, sql)
	if t.queryErr != nil {
		return nil, t.queryErr
	}
	return &eftRows{vals: t.bindings, err: t.rowsErr}, nil
}

func (t *eftTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.sql = append(t.sql, sql)
	return t.doc
}

func TestPGEFTRecordsEFTAuthorization(t *testing.T) {
	at := time.Date(2026, 3, 2, 15, 4, 5, 0, time.UTC)
	one := []eftBinding{{withdrawalID: "w1", amountCents: 125000}}
	badUUID := &pgconn.PgError{Code: "22P02"}
	boom := errors.New("connection reset")

	tests := []struct {
		name    string
		tx      *eftTx
		want    EFTAuthorization
		wantErr error
		other   bool // a non-sentinel error that must not read as ErrNotFound
		queries int
	}{
		{
			name:    "final record bound to one withdrawal",
			tx:      &eftTx{bindings: one, doc: eftDocRow{by: "u-lic", at: at}},
			want:    EFTAuthorization{ID: "doc-1", WithdrawalID: "w1", AmountCents: 125000, SignedBy: "u-lic", SignedAt: at},
			queries: 2,
		},
		{name: "unbound record", tx: &eftTx{doc: eftDocRow{by: "u", at: at}}, wantErr: ErrNotFound, queries: 1},
		{
			name:    "record bound to two withdrawals",
			tx:      &eftTx{bindings: []eftBinding{{"w1", 1}, {"w2", 2}}, doc: eftDocRow{by: "u", at: at}},
			wantErr: ErrNotFound,
			queries: 1,
		},
		{name: "draft or archived document", tx: &eftTx{bindings: one, doc: eftDocRow{err: pgx.ErrNoRows}}, wantErr: ErrNotFound, queries: 2},
		{name: "id is not a uuid", tx: &eftTx{queryErr: badUUID}, wantErr: ErrNotFound, queries: 1},
		{name: "id is not a uuid during iteration", tx: &eftTx{rowsErr: badUUID}, wantErr: ErrNotFound, queries: 1},
		{name: "binding query fails", tx: &eftTx{queryErr: boom}, wantErr: boom, other: true, queries: 1},
		{name: "rows fail", tx: &eftTx{rowsErr: boom}, wantErr: boom, other: true, queries: 1},
		{name: "document query fails", tx: &eftTx{bindings: one, doc: eftDocRow{err: boom}}, wantErr: boom, other: true, queries: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PGEFTRecords{}.EFTAuthorization(context.Background(), tt.tx, "doc-1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.other && errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, a database failure must not read as ErrNotFound", err)
			}
			if tt.wantErr != nil && got != (EFTAuthorization{}) {
				t.Fatalf("got %+v on error, want zero value", got)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if len(tt.tx.sql) != tt.queries {
				t.Fatalf("ran %d queries, want %d", len(tt.tx.sql), tt.queries)
			}
			if !strings.Contains(tt.tx.sql[0], "form_9a_doc_id") {
				t.Fatalf("first query %q does not read the Form 9A binding", tt.tx.sql[0])
			}
			if tt.queries == 2 && !strings.Contains(tt.tx.sql[1], "status IN ('final', 'sent')") {
				t.Fatalf("document query %q does not require a final document", tt.tx.sql[1])
			}
		})
	}
}
