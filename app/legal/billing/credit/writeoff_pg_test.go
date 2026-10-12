package credit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/gate"
)

// valsRow is one scripted row: Scan copies vals into dest in order, or returns err.
type valsRow struct {
	vals []any
	err  error
}

func (r valsRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan: %d dest for %d columns", len(dest), len(r.vals))
	}
	for i, v := range r.vals {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *int64:
			*d = v.(int64)
		case *bool:
			*d = v.(bool)
		case **time.Time:
			*d = v.(*time.Time)
		default:
			return fmt.Errorf("scan: unsupported dest %T", dest[i])
		}
	}
	return nil
}

// pgDraft is one scripted billing.write_off_draft row.
type pgDraft struct {
	w                      WriteOff
	split                  WriteOffBuckets
	creditGate, secondGate string
}

// pgBill is one scripted billing.bill row with its balance and outstanding buckets.
type pgBill struct {
	matter, payer      string
	delivered          bool
	tax, total, open   int64
	outstanding        WriteOffBuckets
	balance            int64           // bill_balance net of reserved, for Draft
	issued             WriteOffBuckets // the bill's components after discount
	transfer           int64           // trust fee transfers net of excess
	outstandingFailure error
}

// insertedWriteOff is one billing.write_off row SaveApproved inserted.
type insertedWriteOff struct {
	id, bill, reason, approval string
	amount                     int64
	split                      WriteOffBuckets
}

// draftTx answers the PGWriteOffs statements from scripted rows and applies their WHERE
// clauses to them, so the WriteOffStore cases of writeoff_effect_test.go run against the
// store's SQL. Unknown statements fail the call; execErr fails every Exec.
type draftTx struct {
	pgx.Tx
	drafts   map[string]*pgDraft
	bills    map[string]pgBill
	inserted map[string]insertedWriteOff
	execErr  error
	sql      []string
	nextID   int
}

func newDraftTx() *draftTx {
	return &draftTx{
		drafts: map[string]*pgDraft{"w1": {
			w:          WriteOff{ID: "w1", BillID: "b1", AmountCents: 59000, Reason: "uncollectible", Status: StatusDraft, PreparedBy: "clerk", ArmsLength: ArmsLengthYes},
			split:      WriteOffBuckets{Fee: 45000, Dtx: 5000, Dnt: 2500, Tax: 6500},
			creditGate: "gb5",
		}},
		bills: map[string]pgBill{"b1": {matter: "m1", payer: "client1", delivered: true, tax: 13000, total: 118000, open: 200000,
			outstanding: WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000}, balance: 118000}},
		inserted: map[string]insertedWriteOff{},
	}
}

var errMalformed = &pgconn.PgError{Code: "22P02"}

func (t *draftTx) draft(id any) (*pgDraft, error) {
	if id == "not-a-uuid" {
		return nil, errMalformed
	}
	d, ok := t.drafts[id.(string)]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return d, nil
}

func (t *draftTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.sql = append(t.sql, sql)
	switch sql {
	case writeOffLockSQL:
		d, err := t.draft(args[0])
		if err != nil {
			return valsRow{err: err}
		}
		return valsRow{vals: []any{d.w.ID, d.w.BillID, d.w.AmountCents, d.w.Reason, string(d.w.Status), d.w.PreparedBy, d.w.ApprovedBy, d.w.ApprovedAt, d.split.Interest, string(d.w.ArmsLength)}}
	case writeOffAwaitingSQL:
		d, err := t.draft(args[0])
		if err != nil {
			return valsRow{err: err}
		}
		return valsRow{vals: []any{d.secondGate}}
	case writeOffApprovalReadSQL:
		d, err := t.draft(args[0])
		if err != nil {
			return valsRow{err: err}
		}
		return valsRow{vals: []any{d.w.BillID, d.w.AmountCents, string(d.w.Status), d.w.PreparedBy, d.w.ApprovedBy, d.creditGate,
			d.split.Fee, d.split.Dtx, d.split.Dnt, d.split.Tax, d.split.Interest, d.w.Reason}}
	case writeOffOutstandingSQL:
		b, ok := t.bills[args[0].(string)]
		if !ok {
			return valsRow{err: pgx.ErrNoRows}
		}
		if b.outstandingFailure != nil {
			return valsRow{err: b.outstandingFailure}
		}
		o := b.outstanding
		i := b.issued
		return valsRow{vals: []any{o.Fee, o.Dtx, o.Dnt, o.Tax, o.Interest, b.balance, i.Fee, i.Dtx, i.Dnt, i.Tax, b.transfer}}
	case writeOffDraftInsertSQL:
		t.nextID++
		id := fmt.Sprintf("w%d", 100+t.nextID)
		t.drafts[id] = &pgDraft{
			w: WriteOff{ID: id, BillID: args[0].(string), AmountCents: args[1].(int64), Reason: args[7].(string),
				Status: StatusDraft, PreparedBy: args[8].(string), ArmsLength: ArmsLength(args[9].(string))},
			split: WriteOffBuckets{Fee: args[2].(int64), Dtx: args[3].(int64), Dnt: args[4].(int64), Tax: args[5].(int64), Interest: args[6].(int64)},
		}
		return valsRow{vals: []any{id}}
	case writeOffBillSQL:
		if args[0] == "not-a-uuid" {
			return valsRow{err: errMalformed}
		}
		b, ok := t.bills[args[0].(string)]
		if !ok {
			return valsRow{err: pgx.ErrNoRows}
		}
		return valsRow{vals: []any{args[0].(string), b.matter, b.payer, b.delivered, b.tax, b.total, b.open}}
	}
	return valsRow{err: fmt.Errorf("unexpected query %q", sql)}
}

func (t *draftTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.sql = append(t.sql, sql)
	if t.execErr != nil {
		return pgconn.CommandTag{}, t.execErr
	}
	none, one := pgconn.NewCommandTag("UPDATE 0"), pgconn.NewCommandTag("UPDATE 1")
	switch sql {
	case writeOffMarkSecondSQL, writeOffSubmitSQL:
		d, ok := t.drafts[args[0].(string)]
		if !ok || d.w.Status != StatusDraft {
			return none, nil
		}
		g := &d.secondGate
		if sql == writeOffSubmitSQL {
			g = &d.creditGate
		}
		if *g != "" && *g != args[1].(string) {
			return none, nil
		}
		*g = args[1].(string)
		return one, nil
	case writeOffCloseSQL:
		d, ok := t.drafts[args[0].(string)]
		if !ok || d.w.Status != StatusDraft || d.creditGate == "" {
			return none, nil
		}
		d.w.Status = Status(args[1].(string))
		return one, nil
	case writeOffApproveSQL:
		d, ok := t.drafts[args[0].(string)]
		if !ok || d.w.Status != StatusDraft {
			return none, nil
		}
		at := args[2].(time.Time)
		d.w.Status, d.w.ApprovedBy, d.w.ApprovedAt = StatusApproved, args[1].(string), &at
		return one, nil
	case writeOffInsertSQL:
		id := args[0].(string)
		if _, ok := t.inserted[id]; ok {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		t.inserted[id] = insertedWriteOff{id: id, bill: args[1].(string), amount: args[2].(int64),
			split:  WriteOffBuckets{Fee: args[3].(int64), Dtx: args[4].(int64), Dnt: args[5].(int64), Tax: args[6].(int64), Interest: args[7].(int64)},
			reason: args[8].(string), approval: args[9].(string)}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("unexpected exec %q", sql)
}

// mapLawyers is a ResponsibleLawyers over a fixed map.
type mapLawyers struct {
	m   map[string]string
	err error
}

func (l mapLawyers) ResponsibleLawyer(_ context.Context, _ pgx.Tx, matterID string) (string, error) {
	return l.m[matterID], l.err
}

var pgLawyers = mapLawyers{m: map[string]string{"m1": "lawyer"}}

// The G-B5 cases of writeoff_effect_test.go, with PGWriteOffs as the store and the bills over
// scripted rows instead of memWriteOffs and memBills.
func TestPGWriteOffsEffectCases(t *testing.T) {
	agent := gate.Principal{Kind: gate.PrincipalAgent, RunID: "run1", ActingFor: "lawyer"}
	tests := []struct {
		name       string
		cfg        SecondApprovalConfig
		approver   gate.Principal
		amount     int64 // payload amount
		wantErr    error
		wantSecond bool
	}{
		{"at the threshold approves now and writes billing.write_off", SecondApprovalConfig{}, user("lawyer"), 59000, nil, false},
		{"above the threshold records the second gate", SecondApprovalConfig{WriteOffSecondApprovalCents: 50000}, user("lawyer"), 59000, nil, true},
		{"agent approval writes nothing", SecondApprovalConfig{}, agent, 59000, ErrAgentApproval, false},
		{"preparer approval writes nothing", SecondApprovalConfig{}, user("clerk"), 59000, ErrSelfApproval, false},
		{"non-responsible approver writes nothing", SecondApprovalConfig{}, user("owner"), 59000, ErrNotResponsible, false},
		{"payload amount differs from the stored draft", SecondApprovalConfig{}, user("lawyer"), 1, ErrInvalidState, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newWriteOffRig(t, 59000, tt.cfg)
			store := PGWriteOffs{Lawyers: pgLawyers}
			r.e.Store, r.e.Bills = store, store
			tx := newDraftTx()
			err := r.e.Apply(context.Background(), tx, gb5Approved(tt.amount, tt.approver))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			d := tx.drafts["w1"]
			switch {
			case tt.wantErr != nil:
				if len(tx.inserted) != 0 || d.w.Status != StatusDraft || d.secondGate != "" || len(r.ledger.posts) != 0 {
					t.Fatalf("inserted %v, draft %+v, posts %v: want nothing applied", tx.inserted, d, r.ledger.posts)
				}
			case tt.wantSecond:
				if d.secondGate != "gate-1" || d.w.Status != StatusDraft || len(tx.inserted) != 0 {
					t.Fatalf("draft %+v, inserted %v: want draft awaiting gate-1", d, tx.inserted)
				}
			default:
				want := insertedWriteOff{id: "w1", bill: "b1", amount: 59000, split: d.split, reason: "uncollectible", approval: "gb5"}
				if got := tx.inserted["w1"]; got != want || len(tx.inserted) != 1 {
					t.Fatalf("inserted = %+v, want %+v", tx.inserted, want)
				}
				if d.w.Status != StatusApproved || d.w.ApprovedBy != "lawyer" || d.w.ApprovedAt == nil {
					t.Fatalf("draft = %+v, want approved by lawyer", d.w)
				}
			}
			// Replay: the store's state makes a second run a no-op.
			if tt.wantErr == nil {
				if err := r.e.Apply(context.Background(), tx, gb5Approved(tt.amount, tt.approver)); err != nil {
					t.Fatalf("replay: %v", err)
				}
				if len(tx.inserted) > 1 || len(r.ledger.posts) > 1 {
					t.Fatalf("replay applied again: inserted %v posts %v", tx.inserted, r.ledger.posts)
				}
			}
		})
	}
}

func TestPGWriteOffsLock(t *testing.T) {
	at := time.Date(2026, 11, 15, 0, 0, 0, 0, toronto)
	tests := []struct {
		name    string
		id      string
		mutate  func(*pgDraft)
		want    WriteOff
		wantErr error
	}{
		{name: "draft", id: "w1", want: WriteOff{ID: "w1", BillID: "b1", AmountCents: 59000, Reason: "uncollectible", Status: StatusDraft, PreparedBy: "clerk", ArmsLength: ArmsLengthYes}},
		{name: "approved", id: "w1", mutate: func(d *pgDraft) { d.w.Status, d.w.ApprovedBy, d.w.ApprovedAt = StatusApproved, "lawyer", &at },
			want: WriteOff{ID: "w1", BillID: "b1", AmountCents: 59000, Reason: "uncollectible", Status: StatusApproved, PreparedBy: "clerk", ApprovedBy: "lawyer", ApprovedAt: &at, ArmsLength: ArmsLengthYes}},
		{name: "approved without approver fails closed", id: "w1", mutate: func(d *pgDraft) { d.w.Status = StatusApproved }, wantErr: ErrInvalidState},
		{name: "unknown status fails closed", id: "w1", mutate: func(d *pgDraft) { d.w.Status = "void" }, wantErr: ErrInvalidState},
		{name: "missing", id: "w9", wantErr: ErrNotFound},
		{name: "malformed id", id: "not-a-uuid", wantErr: ErrNotFound},
		{name: "empty id", id: "", wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			if tt.mutate != nil {
				tt.mutate(tx.drafts["w1"])
			}
			got, err := PGWriteOffs{}.Lock(context.Background(), tx, tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.ID != tt.want.ID || got.Status != tt.want.Status || got.ApprovedBy != tt.want.ApprovedBy || got.PreparedBy != tt.want.PreparedBy ||
				got.AmountCents != tt.want.AmountCents || (got.ApprovedAt == nil) != (tt.want.ApprovedAt == nil) {
				t.Fatalf("Lock = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPGWriteOffsSecondGate(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		approved  bool
		id, gate  string
		wantErr   error
		wantStore string
	}{
		{name: "sets the gate", id: "w1", gate: "g2", wantStore: "g2"},
		{name: "same gate again is idempotent", existing: "g2", id: "w1", gate: "g2", wantStore: "g2"},
		{name: "another gate is refused", existing: "g2", id: "w1", gate: "g3", wantErr: ErrInvalidState, wantStore: "g2"},
		{name: "approved write-off is refused", approved: true, id: "w1", gate: "g2", wantErr: ErrInvalidState},
		{name: "missing write-off is refused", id: "w9", gate: "g2", wantErr: ErrInvalidState},
		{name: "empty gate is refused", id: "w1", gate: "", wantErr: ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			tx.drafts["w1"].secondGate = tt.existing
			if tt.approved {
				tx.drafts["w1"].w.Status = StatusApproved
			}
			err := PGWriteOffs{}.MarkAwaitingSecond(context.Background(), tx, tt.id, tt.gate)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			got, err := PGWriteOffs{}.AwaitingSecond(context.Background(), tx, "w1")
			if err != nil || got != tt.wantStore {
				t.Fatalf("AwaitingSecond = %q, %v; want %q", got, err, tt.wantStore)
			}
		})
	}
	if _, err := (PGWriteOffs{}).AwaitingSecond(context.Background(), newDraftTx(), "w9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AwaitingSecond missing: %v, want ErrNotFound", err)
	}
}

func TestPGWriteOffsSaveApproved(t *testing.T) {
	at := time.Date(2026, 11, 15, 0, 0, 0, 0, toronto)
	approved := WriteOff{ID: "w1", BillID: "b1", AmountCents: 59000, Reason: "uncollectible", Status: StatusApproved,
		PreparedBy: "clerk", ApprovedBy: "lawyer", ApprovedAt: &at}
	boom := errors.New("connection reset")
	tests := []struct {
		name       string
		w          func(WriteOff) WriteOff
		mutate     func(*draftTx)
		wantErr    error
		wantInsert bool
	}{
		{name: "approves and inserts", wantInsert: true},
		{name: "already approved by the same approver is idempotent", mutate: func(tx *draftTx) {
			d := tx.drafts["w1"]
			d.w.Status, d.w.ApprovedBy, d.w.ApprovedAt = StatusApproved, "lawyer", &at
		}, wantInsert: true},
		{name: "approved by someone else is refused", mutate: func(tx *draftTx) {
			d := tx.drafts["w1"]
			d.w.Status, d.w.ApprovedBy, d.w.ApprovedAt = StatusApproved, "owner", &at
		}, wantErr: ErrInvalidState},
		{name: "no G-B5 gate fails closed", mutate: func(tx *draftTx) { tx.drafts["w1"].creditGate = "" }, wantErr: ErrInvalidState},
		{name: "split not summing fails closed", mutate: func(tx *draftTx) { tx.drafts["w1"].split.Fee-- }, wantErr: ErrInvalidState},
		{name: "amount differs from the draft", w: func(w WriteOff) WriteOff { w.AmountCents = 1; return w }, wantErr: ErrInvalidState},
		{name: "bill differs from the draft", w: func(w WriteOff) WriteOff { w.BillID = "b2"; return w }, wantErr: ErrInvalidState},
		{name: "preparer differs from the draft", w: func(w WriteOff) WriteOff { w.PreparedBy = "x"; return w }, wantErr: ErrInvalidState},
		{name: "preparer approves", w: func(w WriteOff) WriteOff { w.ApprovedBy = "clerk"; return w }, wantErr: ErrSelfApproval},
		{name: "not approved", w: func(w WriteOff) WriteOff { w.Status = StatusDraft; return w }, wantErr: ErrInvalidState},
		{name: "no approval time", w: func(w WriteOff) WriteOff { w.ApprovedAt = nil; return w }, wantErr: ErrInvalidState},
		{name: "missing draft", w: func(w WriteOff) WriteOff { w.ID = "w9"; return w }, wantErr: ErrNotFound},
		{name: "write error is returned", mutate: func(tx *draftTx) { tx.execErr = boom }, wantErr: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			if tt.mutate != nil {
				tt.mutate(tx)
			}
			w := approved
			if tt.w != nil {
				w = tt.w(w)
			}
			err := PGWriteOffs{}.SaveApproved(context.Background(), tx, w)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := len(tx.inserted) == 1; got != tt.wantInsert {
				t.Fatalf("inserted = %v, want insert %v", tx.inserted, tt.wantInsert)
			}
			if tt.wantErr != nil {
				return
			}
			// A second call changes nothing.
			if err := (PGWriteOffs{}).SaveApproved(context.Background(), tx, w); err != nil || len(tx.inserted) != 1 {
				t.Fatalf("second SaveApproved: %v, inserted %v", err, tx.inserted)
			}
			if got := tx.inserted["w1"]; got.approval != "gb5" || got.split.Sum() != 59000 {
				t.Fatalf("inserted = %+v, want approval gb5 and a split of 59000", got)
			}
		})
	}
}

func TestSplitWriteOff(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		open    WriteOffBuckets
		want    WriteOffBuckets
		wantErr error
	}{
		{"proportional", 59000, WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000},
			WriteOffBuckets{Fee: 45000, Dtx: 5000, Dnt: 2500, Tax: 6500}, nil},
		{"whole balance", 118000, WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000},
			WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000}, nil},
		{"largest remainder, ties to the first bucket", 1, WriteOffBuckets{Fee: 1, Tax: 1}, WriteOffBuckets{Fee: 1}, nil},
		{"leftover cents go to the largest remainders", 10, WriteOffBuckets{Fee: 100, Dtx: 100, Tax: 13},
			WriteOffBuckets{Fee: 5, Dtx: 5}, nil},
		{"interest bucket", 300, WriteOffBuckets{Fee: 200, Interest: 100}, WriteOffBuckets{Fee: 200, Interest: 100}, nil},
		{"exceeds outstanding", 118001, WriteOffBuckets{Fee: 90000, Dtx: 10000, Dnt: 5000, Tax: 13000}, WriteOffBuckets{}, ErrExceedsBalance},
		{"nothing outstanding", 1, WriteOffBuckets{}, WriteOffBuckets{}, ErrExceedsBalance},
		{"negative bucket fails closed", 1, WriteOffBuckets{Fee: 5, Tax: -1}, WriteOffBuckets{}, ErrInvalidState},
		{"zero amount", 0, WriteOffBuckets{Fee: 5}, WriteOffBuckets{}, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitWriteOff(tt.amount, tt.open)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("split = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPGWriteOffsDraft(t *testing.T) {
	boom := errors.New("connection reset")
	base := WriteOff{BillID: "b1", AmountCents: 59000, Reason: "uncollectible", PreparedBy: "clerk"}
	tests := []struct {
		name      string
		w         func(WriteOff) WriteOff
		mutate    func(*pgBill)
		wantErr   error
		wantSplit WriteOffBuckets
	}{
		{name: "split computed at draft time", wantSplit: WriteOffBuckets{Fee: 45000, Dtx: 5000, Dnt: 2500, Tax: 6500}},
		{name: "buckets not matching the balance fail closed", mutate: func(b *pgBill) { b.balance = 117000 }, wantErr: ErrInvalidState},
		{name: "above the outstanding balance", w: func(w WriteOff) WriteOff { w.AmountCents = 118001; return w }, wantErr: ErrExceedsBalance},
		{name: "missing bill", w: func(w WriteOff) WriteOff { w.BillID = "b9"; return w }, wantErr: ErrNotFound},
		{name: "read error", mutate: func(b *pgBill) { b.outstandingFailure = boom }, wantErr: boom},
		{name: "no preparer", w: func(w WriteOff) WriteOff { w.PreparedBy = ""; return w }, wantErr: ErrInvalidState},
		{name: "no reason", w: func(w WriteOff) WriteOff { w.Reason = ""; return w }, wantErr: ErrInvalidState},
		{name: "zero amount", w: func(w WriteOff) WriteOff { w.AmountCents = 0; return w }, wantErr: ErrInvalidAmount},
		// rule: BIL-T-080
		{name: "unknown arm's-length value", w: func(w WriteOff) WriteOff { w.ArmsLength = "maybe"; return w }, wantErr: ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			if tt.mutate != nil {
				b := tx.bills["b1"]
				tt.mutate(&b)
				tx.bills["b1"] = b
			}
			w := base
			if tt.w != nil {
				w = tt.w(w)
			}
			got, err := PGWriteOffs{}.Draft(context.Background(), tx, w)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(tx.drafts) != 1 {
					t.Fatalf("a draft was stored: %v", tx.drafts)
				}
				return
			}
			d := tx.drafts[got.ID]
			if d == nil || got.Status != StatusDraft || d.split != tt.wantSplit || d.w.PreparedBy != "clerk" {
				t.Fatalf("draft = %+v (%+v), want split %+v", got, d, tt.wantSplit)
			}
			if err := (PGWriteOffs{}).Submit(context.Background(), tx, got.ID, "g5"); err != nil || d.creditGate != "g5" {
				t.Fatalf("Submit: %v, gate %q", err, d.creditGate)
			}
			if err := (PGWriteOffs{}).Submit(context.Background(), tx, got.ID, "g6"); !errors.Is(err, ErrInvalidState) || d.creditGate != "g5" {
				t.Fatalf("second Submit: %v, gate %q; want refused", err, d.creditGate)
			}
		})
	}
}

func TestPGWriteOffsBill(t *testing.T) {
	boom := errors.New("matter down")
	tests := []struct {
		name    string
		id      string
		lawyers ResponsibleLawyers
		mutate  func(*pgBill)
		want    WriteOffBill
		wantErr error
	}{
		{name: "reads the bill and its responsible lawyer", id: "b1", lawyers: pgLawyers,
			want: WriteOffBill{Bill: Bill{ID: "b1", Delivered: true, OpenCents: 200000, ResponsibleUser: "lawyer"}, TaxCents: 13000, TotalCents: 118000, ClientID: "client1"}},
		{name: "no lawyer lookup fails closed", id: "b1", wantErr: ErrInvalidState},
		{name: "no responsible lawyer fails closed", id: "b1", lawyers: mapLawyers{m: map[string]string{}}, wantErr: ErrInvalidState},
		{name: "lookup error", id: "b1", lawyers: mapLawyers{err: boom}, wantErr: boom},
		{name: "negative balance fails closed", id: "b1", lawyers: pgLawyers, mutate: func(b *pgBill) { b.open = -1 }, wantErr: ErrInvalidState},
		{name: "missing bill", id: "b9", lawyers: pgLawyers, wantErr: ErrNotFound},
		{name: "malformed id", id: "not-a-uuid", lawyers: pgLawyers, wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			if tt.mutate != nil {
				b := tx.bills["b1"]
				tt.mutate(&b)
				tx.bills["b1"] = b
			}
			got, err := PGWriteOffs{Lawyers: tt.lawyers}.WriteOffBill(context.Background(), tx, tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("bill = %+v, want %+v", got, tt.want)
			}
		})
	}
}
