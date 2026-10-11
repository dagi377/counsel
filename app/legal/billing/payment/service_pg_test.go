package payment

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/server"
)

// scriptTx answers each SQL constant with its scripted rows and records every Exec.
type scriptTx struct {
	pgx.Tx
	row   map[string][]any   // QueryRow answers; missing means no row
	rows  map[string][][]any // Query answers
	execs []string
	args  [][]any
}

func (t *scriptTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	v, ok := t.row[sql]
	if !ok {
		return scriptRow{err: pgx.ErrNoRows}
	}
	return scriptRow{vals: v}
}

func (t *scriptTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	return &scriptRows{vals: t.rows[sql], i: -1}, nil
}

func (t *scriptTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	t.args = append(t.args, args)
	return pgconn.CommandTag{}, nil
}

type scriptRow struct {
	vals []any
	err  error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		reflect.ValueOf(d).Elem().Set(reflect.ValueOf(r.vals[i]))
	}
	return nil
}

type scriptRows struct {
	pgx.Rows
	vals [][]any
	i    int
}

func (r *scriptRows) Next() bool                                   { r.i++; return r.i < len(r.vals) }
func (r *scriptRows) Close()                                       {}
func (r *scriptRows) Err() error                                   { return nil }
func (r *scriptRows) Scan(dest ...any) error                       { return scriptRow{vals: r.vals[r.i]}.Scan(dest...) }
func (r *scriptRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *scriptRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }

// txOnce runs fn on its one scripted tx.
type txOnce struct{ tx pgx.Tx }

func (d txOnce) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

func (t *scriptTx) wrote(sql string) bool {
	for _, s := range t.execs {
		if s == sql {
			return true
		}
	}
	return false
}

// openBillRow is a delivered bill of client c1 with 90400 cents open, 10400 of it tax.
func openBillRow(payer, status string) []any { return []any{payer, status, int64(90400), int64(10400)} }

func TestPGServiceCapture(t *testing.T) {
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	base := Capture{ClientID: "c1", BillID: "b1", Kind: KindBillPayment, Method: "eft", RecordedBy: "u1", ReceivedOn: on}
	with := func(f func(*Capture)) Capture { c := base; f(&c); return c }
	tests := []struct {
		name      string
		c         Capture
		bill      []any
		want      []Allocation
		wantErr   error
		wantWrite bool
	}{
		{name: "partial tax share", c: with(func(c *Capture) { c.AmountCents = 50000 }), bill: openBillRow("c1", "delivered"),
			want: []Allocation{{PaymentID: "pay-1", BillID: "b1", FeeCents: 44248, TaxCents: 5752}}, wantWrite: true},
		{name: "overpayment unapplied", c: with(func(c *Capture) { c.AmountCents = 100000 }), bill: openBillRow("c1", "partially_paid"),
			want: []Allocation{{PaymentID: "pay-1", BillID: "b1", FeeCents: 80000, TaxCents: 10400}, {PaymentID: "pay-1", FeeCents: 9600}}, wantWrite: true},
		{name: "retainer", c: with(func(c *Capture) { c.AmountCents = 100; c.Kind = KindRetainer }), wantErr: ErrRetainerToOperating},
		{name: "zero", c: base, wantErr: ErrInvalid},
		{name: "no method", c: with(func(c *Capture) { c.AmountCents = 100; c.Method = "" }), wantErr: ErrInvalid},
		{name: "other client", c: with(func(c *Capture) { c.AmountCents = 100 }), bill: openBillRow("c2", "delivered"), wantErr: ErrOtherClient},
		{name: "draft bill", c: with(func(c *Capture) { c.AmountCents = 100 }), bill: openBillRow("c1", "draft"), wantErr: ErrInvalid},
		{name: "unknown bill", c: with(func(c *Capture) { c.AmountCents = 100 }), wantErr: ErrNotFound},
		{name: "cash unchecked", c: with(func(c *Capture) { c.AmountCents = 100; c.Method = MethodCash; c.MatterID = "m1" }),
			bill: openBillRow("c1", "delivered"), wantErr: ErrCashUnchecked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &scriptTx{row: map[string][]any{insertPaymentSQL: {"pay-1"}}}
			if tt.bill != nil {
				tx.row[lockBillSQL] = tt.bill
			}
			_, got, err := PGService{DB: txOnce{tx}, Payers: payers{"c1": "Client One"}}.Capture(context.Background(), tt.c)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Capture err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("allocations = %+v, want %+v", got, tt.want)
			}
			// rule: DM-045
			var sum int64
			for _, a := range got {
				sum += a.Total()
			}
			if err == nil && sum != tt.c.AmountCents {
				t.Fatalf("sum(allocations) = %d, want %d", sum, tt.c.AmountCents)
			}
			if tx.wrote(insertAllocationSQL) != tt.wantWrite {
				t.Fatalf("allocation written = %v, want %v", !tt.wantWrite, tt.wantWrite)
			}
		})
	}
}

func TestPGServiceCaptureDedup(t *testing.T) {
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		key  string
		c    Capture
	}{
		{name: "provider event", key: byProviderSQL, c: Capture{Provider: "card", ProviderEventID: "evt_1"}},
		{name: "idempotency key", key: byKeySQL, c: Capture{IdempotencyKey: "k1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.c
			c.ClientID, c.BillID, c.AmountCents, c.Method, c.RecordedBy, c.ReceivedOn = "c1", "b1", 1000, "eft", "u1", on
			tx := &scriptTx{
				row: map[string][]any{tt.key: {"pay-0"}, getPaymentSQL: {"pay-0", on, "c1", int64(1000), "eft",
					c.Provider, c.ProviderEventID, "u1", "posted", "", c.IdempotencyKey, "", "b1"}},
				rows: map[string][][]any{allocationsSQL: {{"b1", int64(885), int64(115)}}},
			}
			p, as, err := PGService{DB: txOnce{tx}, Payers: payers{"c1": "Client One"}}.Capture(context.Background(), c)
			if err != nil || p.ID != "pay-0" || len(as) != 1 || as[0].Total() != 1000 {
				t.Fatalf("Capture = %+v, %+v, %v; want the first payment", p, as, err)
			}
			if len(tx.execs) != 0 {
				t.Fatalf("replay wrote %d statements, want none", len(tx.execs))
			}
		})
	}
}

func TestPGServiceReverse(t *testing.T) {
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	row := func(status, reason string) []any {
		return []any{"pay-1", on, "c1", int64(50000), "eft", "", "", "u1", status, reason, "", "u2", "b1"}
	}
	tests := []struct {
		name    string
		status  string
		reason  ReversalReason
		by      Actor
		human   []any // humanSQL answer; nil means no such user
		found   bool
		wantErr error
		writes  int
	}{
		{name: "nsf", status: "posted", reason: ReasonNSF, by: Actor{UserID: "u2"}, human: []any{true}, found: true, writes: 3},
		{name: "chargeback", status: "posted", reason: ReasonChargeback, by: Actor{UserID: "u2"}, human: []any{true}, found: true, writes: 3},
		{name: "again", status: "reversed", reason: ReasonNSF, by: Actor{UserID: "u2"}, human: []any{true}, found: true, wantErr: ErrReversed},
		{name: "unknown", reason: ReasonNSF, by: Actor{UserID: "u2"}, human: []any{true}, wantErr: ErrNotFound},
		{name: "bad reason", reason: "lost", by: Actor{UserID: "u2"}, wantErr: ErrInvalid},
		{name: "agent actor", status: "posted", reason: ReasonNSF, by: Actor{UserID: "u2", Agent: true}, human: []any{true}, found: true, wantErr: ErrAgentReverse},
		{name: "agent principal", status: "posted", reason: ReasonNSF, by: Actor{UserID: "a1"}, human: []any{false}, found: true, wantErr: ErrAgentReverse},
		{name: "no actor", status: "posted", reason: ReasonNSF, found: true, wantErr: ErrInvalid},
		{name: "unknown actor", status: "posted", reason: ReasonNSF, by: Actor{UserID: "u9"}, found: true, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &scriptTx{row: map[string][]any{}}
			if tt.human != nil {
				tx.row[humanSQL] = tt.human
			}
			if tt.found {
				tx.row[lockPaymentSQL] = []any{tt.status}
				tx.row[getPaymentSQL] = row("reversed", string(tt.reason))
			}
			p, err := PGService{DB: txOnce{tx}}.Reverse(context.Background(), "pay-1", tt.reason, tt.by)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Reverse err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.execs) != tt.writes {
				t.Fatalf("writes = %d, want %d", len(tx.execs), tt.writes)
			}
			if tt.writes > 0 {
				// rule: BIL-I-019
				if tx.execs[0] != reverseAllocationsSQL || p.Status != StatusReversed || p.Reason != tt.reason || p.ReversedBy != tt.by.UserID {
					t.Fatalf("Reverse = %+v, execs %v", p, tx.execs)
				}
				// The -1 x rows and the reversal row carry the reversing user, never the allocator.
				if got := tx.args[0][1]; got != tt.by.UserID {
					t.Fatalf("reversal allocated_by = %v, want %s", got, tt.by.UserID)
				}
				if tx.execs[1] != insertReversalSQL || tx.args[1][2] != tt.by.UserID {
					t.Fatalf("reversal row = %s %v, want reversed_by %s", tx.execs[1], tx.args[1], tt.by.UserID)
				}
			}
		})
	}
}

func TestPGServiceReadsOperatingOnly(t *testing.T) {
	// rule: BIL-L-034
	// Trust deposits are trust receipts: every read and lock is limited to operating payments.
	for name, sql := range map[string]string{"get": getPaymentSQL, "list": listPaymentsSQL, "lock": lockPaymentSQL,
		"dedup": byProviderSQL, "propose": lockProposedPaymentSQL} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(sql, operating) {
				t.Fatalf("%s sql does not filter operating payments", name)
			}
		})
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestPGServiceGetAndList(t *testing.T) {
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	r := []any{"pay-1", on, "c1", int64(50000), "eft", "", "", "u1", "posted", "", "", "", "b1"}
	tx := &scriptTx{row: map[string][]any{getPaymentSQL: r}, rows: map[string][][]any{listPaymentsSQL: {r},
		allocationsSQL: {{"b1", int64(44248), int64(5752)}}}}
	s := PGService{DB: txOnce{tx}}
	ctx := context.Background()
	p, err := s.Get(ctx, "pay-1")
	if err != nil || p.Status != StatusCaptured || p.Capture.BillID != "b1" || p.Capture.ReceivedOn.Location().String() != "America/Toronto" {
		t.Fatalf("Get = %+v, %v", p, err)
	}
	if _, err := (PGService{DB: txOnce{&scriptTx{}}}).Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get unknown err = %v", err)
	}
	ps, err := s.List(ctx, ListFilter{Limit: 10})
	if err != nil || len(ps) != 1 || ps[0].ID != "pay-1" {
		t.Fatalf("List = %+v, %v", ps, err)
	}
	as, err := s.Allocations(ctx, "pay-1")
	if err != nil || len(as) != 1 || as[0].Total() != 50000 {
		t.Fatalf("Allocations = %+v, %v", as, err)
	}
}

func TestPGProposalsPropose(t *testing.T) {
	bills := []ProposedAllocation{{TargetType: "bill", TargetID: "b1", AmountCents: 50000}}
	tests := []struct {
		name    string
		version int
		rows    []ProposedAllocation
		payment []any
		bill    []any
		want    []Allocation
		wantErr error
	}{
		{name: "bills only saved as proposed", version: 1, rows: bills, payment: []any{"c1", int64(50000), "posted"},
			bill: openBillRow("c1", "delivered"), want: []Allocation{{PaymentID: "pay-1", BillID: "b1", FeeCents: 44248, TaxCents: 5752}}},
		{name: "stale version", version: 2, rows: bills, wantErr: server.ErrPreconditionFailed},
		{name: "trust ledger", version: 1, rows: []ProposedAllocation{{TargetType: "trust_ledger", TargetID: "l1", AmountCents: 1}}, wantErr: ErrInvalid},
		{name: "unapplied", version: 1, rows: []ProposedAllocation{{TargetType: "unapplied", AmountCents: 1}}, wantErr: ErrInvalid},
		{name: "over the payment", version: 1, rows: bills, payment: []any{"c1", int64(40000), "posted"}, wantErr: ErrInvalid},
		{name: "over the bill", version: 1, rows: []ProposedAllocation{{TargetType: "bill", TargetID: "b1", AmountCents: 90401}},
			payment: []any{"c1", int64(100000), "posted"}, bill: openBillRow("c1", "delivered"), wantErr: ErrInvalid},
		{name: "other client", version: 1, rows: bills, payment: []any{"c1", int64(50000), "posted"}, bill: openBillRow("c2", "delivered"), wantErr: ErrOtherClient},
		{name: "reversed", version: 1, rows: bills, payment: []any{"c1", int64(50000), "reversed"}, wantErr: ErrInvalid},
		{name: "unknown payment", version: 1, rows: bills, wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &scriptTx{row: map[string][]any{}}
			if tt.payment != nil {
				tx.row[lockProposedPaymentSQL] = tt.payment
			}
			if tt.bill != nil {
				tx.row[lockBillSQL] = tt.bill
			}
			got, err := PGProposals{}.Propose(context.Background(), tx, "pay-1", tt.version, tt.rows, "u1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Propose err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Propose = %+v, want %+v", got, tt.want)
			}
			// Applying is the effect's: a proposal never writes billing.payment_allocation.
			if tx.wrote(insertAllocationSQL) {
				t.Fatal("proposal applied an allocation")
			}
			if err == nil && (!tx.wrote(supersedeProposalSQL) || !tx.wrote(insertProposalSQL)) {
				t.Fatalf("execs = %v, want supersede then insert", tx.execs)
			}
		})
	}
}

// payers is a PayerNames over a fixed client -> name map.
type payers map[string]string

func (p payers) PayerName(_ context.Context, id string) (string, error) {
	if n, ok := p[id]; ok {
		return n, nil
	}
	return "", errPayerMissing
}

var errPayerMissing = errors.New("no such payer")

func TestPGServiceCapturePayerName(t *testing.T) {
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	c := Capture{ClientID: "c1", BillID: "b1", Kind: KindBillPayment, Method: "eft", RecordedBy: "u1", ReceivedOn: on, AmountCents: 100}
	tests := []struct {
		name     string
		payers   PayerNames
		wantName string
		wantErr  error
	}{
		{name: "named from party", payers: payers{"c1": "Client One"}, wantName: "Client One"},
		{name: "port not wired", wantErr: ErrNoPayerNames},
		{name: "unknown payer", payers: payers{}, wantErr: errPayerMissing},
		{name: "empty name", payers: payers{"c1": ""}, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &scriptTx{row: map[string][]any{insertPaymentSQL: {"pay-1"}, lockBillSQL: openBillRow("c1", "delivered")}}
			var inserted []any
			rec := &recordTx{scriptTx: tx, sql: insertPaymentSQL, args: &inserted}
			_, _, err := PGService{DB: txOnce{rec}, Payers: tt.payers}.Capture(context.Background(), c)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Capture err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if inserted != nil || len(tx.execs) != 0 {
					t.Fatal("capture wrote without a payer name")
				}
				return
			}
			if got := inserted[len(inserted)-1]; got != tt.wantName {
				t.Fatalf("payer_name = %v, want %q", got, tt.wantName)
			}
		})
	}
}

// recordTx records the args of one QueryRow sql.
type recordTx struct {
	*scriptTx
	sql  string
	args *[]any
}

func (r *recordTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if sql == r.sql {
		*r.args = args
	}
	return r.scriptTx.QueryRow(ctx, sql, args...)
}
