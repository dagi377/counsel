package credit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// memNotes is an in-memory NoteStore.
type memNotes struct {
	n      map[string]StoredNote
	bucket Bucket
}

func (m *memNotes) Lock(_ context.Context, _ pgx.Tx, id string) (StoredNote, error) {
	n, ok := m.n[id]
	if !ok {
		return StoredNote{}, ErrNotFound
	}
	return n, nil
}

func (m *memNotes) Bucket(_ context.Context, _ pgx.Tx, _, _ string) (Bucket, error) {
	return m.bucket, nil
}

func (m *memNotes) Issue(_ context.Context, _ pgx.Tx, id, number, approvalID string, at time.Time) error {
	n := m.n[id]
	n.Status, n.Number, n.ApprovalID, n.IssuedAt = noteStatusIssued, number, approvalID, &at
	m.n[id] = n
	return nil
}

// noteBills is an in-memory Bills. creditfake implements the same interface, but it imports
// this package, so an internal test cannot use it.
type noteBills map[string]Bill

func (m noteBills) Bill(_ context.Context, id string) (Bill, error) {
	b, ok := m[id]
	if !ok {
		return Bill{}, ErrNotFound
	}
	return b, nil
}

// memNumbers counts up per series.
type memNumbers map[string]int

func (m memNumbers) Next(_ context.Context, _ pgx.Tx, series string) (string, error) {
	m[series]++
	return "CN-" + strconv.Itoa(m[series]), nil
}

type noteRig struct {
	e     *NoteEffect
	notes *memNotes
	gl    *ledgerfake.Fake
}

// newNoteRig has delivered bill b1 with $1,000 pre-tax billed and draft note n1 crediting
// $200 plus $26 HST.
func newNoteRig(prior int64) noteRig {
	bills := noteBills{
		"b1": {ID: "b1", Delivered: true, OpenCents: 113000, ResponsibleUser: "lawyer"},
		"b2": {ID: "b2", Delivered: false, OpenCents: 113000, ResponsibleUser: "lawyer"},
	}
	notes := &memNotes{
		n: map[string]StoredNote{
			"n1": {ID: "n1", BillID: "b1", Kind: KindFeeReduction, AmountCents: 20000, TaxCents: 2600, Reason: "r", Status: noteStatusPending},
			"n2": {ID: "n2", BillID: "b2", Kind: KindFeeReduction, AmountCents: 20000, TaxCents: 2600, Reason: "r", Status: noteStatusDraft},
		},
		bucket: Bucket{Key: "bill", BilledCents: 100000, CreditedCents: prior},
	}
	clk := clockfake.New(effectNow, toronto)
	gl := ledgerfake.New(clock.DateOf(effectNow, toronto), books.AcctFeeAdjustments, books.AcctSalesTaxPayable, books.AcctARFees)
	return noteRig{
		e:     &NoteEffect{Store: notes, Bills: bills, Numbers: memNumbers{}, Ledger: gl, Clock: clk},
		notes: notes,
		gl:    gl,
	}
}

func noteGate(id, note, bill string, amount int64, kind string, approver gate.Principal) registry.Approved {
	p, _ := json.Marshal(creditNotePayload{BillID: bill, AmountCents: amount, Kind: kind, Reason: "r"})
	return registry.Approved{
		GateID: id, Kind: GateKindCreditNote, Version: 1,
		Subject: registry.Subject{Kind: "billing.credit_note", ID: note},
		Payload: p, Preparer: user("clerk"), Approvers: []gate.Principal{approver},
	}
}

func TestNoteEffectApply(t *testing.T) {
	agent := gate.Principal{Kind: gate.PrincipalAgent, RunID: "run", ActingFor: "lawyer"}
	tests := []struct {
		name      string
		prior     int64
		g         registry.Approved
		want      error
		wantCode  string
		wantPosts int
	}{
		{"approves and posts", 0, noteGate("g1", "n1", "b1", 20000, "credit_note", user("lawyer")), nil, "", 1},
		{"agent approver", 0, noteGate("g1", "n1", "b1", 20000, "credit_note", agent), ErrAgentApproval, "", 0},
		{"preparer approves", 0, noteGate("g1", "n1", "b1", 20000, "credit_note", user("clerk")), ErrSelfApproval, "", 0},
		// rule: BIL-C-082
		{"over-credit is 409 credit_exceeds_bill", 90000, noteGate("g1", "n1", "b1", 20000, "credit_note", user("lawyer")), ErrExceedsBalance, CodeCreditExceedsBill, 0},
		{"bill not delivered", 0, noteGate("g1", "n2", "b2", 20000, "credit_note", user("lawyer")), ErrBillNotDelivered, "", 0},
		{"unknown note", 0, noteGate("g1", "nx", "b1", 20000, "credit_note", user("lawyer")), ErrNotFound, "", 0},
		{"payload does not match", 0, noteGate("g1", "n1", "b1", 1, "credit_note", user("lawyer")), ErrInvalidState, "", 0},
		{"write-off payload", 0, noteGate("g1", "n1", "b1", 20000, "write_off", user("lawyer")), ErrInvalidState, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newNoteRig(tt.prior)
			err := NoteEffects(r.e)[GateKindCreditNote](context.Background(), nil, tt.g)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
			} else {
				if !errors.Is(err, tt.want) || !errors.Is(err, gate.ErrEffectFailed) {
					t.Fatalf("err = %v, want %v wrapped in ErrEffectFailed", err, tt.want)
				}
			}
			if tt.wantCode != "" {
				var p *api.Problem
				if !errors.As(err, &p) || p.Status != http.StatusConflict || p.Code != tt.wantCode || p.Rule != "BIL-C-082" {
					t.Fatalf("problem = %+v, want 409 %s", p, tt.wantCode)
				}
			}
			bal, _ := r.gl.Balance(context.Background(), books.AcctARFees, clock.DateOf(effectNow, toronto))
			if got := tt.wantPosts; (got == 1) != (bal == -22600) {
				t.Fatalf("AR balance = %d, want posts %d", bal, got)
			}
			n := r.notes.n["n1"]
			if tt.want == nil && (n.Status != noteStatusIssued || n.Number != "CN-1" || n.ApprovalID != "g1") {
				t.Fatalf("note = %+v, want issued CN-1 under g1", n)
			}
			if tt.want != nil && n.Status == noteStatusIssued {
				t.Fatalf("note issued on error: %+v", n)
			}
		})
	}
}

func TestNoteEffectReplayChangesNothing(t *testing.T) {
	r := newNoteRig(0)
	g := noteGate("g1", "n1", "b1", 20000, "credit_note", user("lawyer"))
	apply := NoteEffects(r.e)[GateKindCreditNote]
	for i := range 2 {
		if err := apply(context.Background(), nil, g); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	bal, err := r.gl.Balance(context.Background(), books.AcctARFees, clock.DateOf(effectNow, toronto))
	if err != nil || bal != -22600 {
		t.Fatalf("AR = %d, %v; want one post of -22600", bal, err)
	}
	if n := r.notes.n["n1"]; n.Number != "CN-1" {
		t.Fatalf("number = %s, want CN-1 (no second number)", n.Number)
	}
	// Another gate on an issued note is refused.
	if err := apply(context.Background(), nil, noteGate("g2", "n1", "b1", 20000, "credit_note", user("lawyer"))); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second gate err = %v, want ErrInvalidState", err)
	}
}

func TestNoteEntryBalances(t *testing.T) {
	tests := []struct {
		name  string
		n     StoredNote
		lines int
	}{
		{"with tax", StoredNote{ID: "n1", AmountCents: 10000, TaxCents: 1300}, 3},
		{"no tax", StoredNote{ID: "n1", AmountCents: 10000}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := noteEntry(tt.n, clock.Date{Year: 2026, Month: 10, Day: 5})
			var dr, cr ledger.Cents
			for _, l := range e.Lines {
				if l.Side == ledger.Debit {
					dr += l.Amount
				} else {
					cr += l.Amount
				}
			}
			if len(e.Lines) != tt.lines || dr != cr || e.Book != books.BookOperating || e.SourceKind != SourceCreditNote {
				t.Fatalf("entry = %+v", e)
			}
		})
	}
}

// TestMergeEffectsNoSlugCollision calls both NoteEffects and Effects: merged, every slug has
// one handler and billing.credit_note dispatches on the payload kind.
func TestMergeEffectsNoSlugCollision(t *testing.T) {
	r := newNoteRig(0)
	var woCalls int
	wo := map[string]registry.EffectFunc{
		GateKindCreditNote:     func(context.Context, pgx.Tx, registry.Approved) error { woCalls++; return nil },
		GateKindWriteOffSecond: func(context.Context, pgx.Tx, registry.Approved) error { return nil },
	}
	real := Effects(&WriteOffEffect{}, SecondApproval{})
	for k := range real {
		if _, ok := wo[k]; !ok {
			t.Fatalf("Effects slug %s not covered by the test map", k)
		}
	}
	notes := NoteEffects(r.e)
	merged, err := MergeEffects(notes, wo)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged slugs = %d, want 2", len(merged))
	}
	tests := []struct {
		name    string
		g       registry.Approved
		woCalls int
		issued  bool
	}{
		{"write-off goes to write-off effect", noteGate("g0", "w1", "b1", 500, "write_off", user("lawyer")), 1, false},
		{"credit note goes to note effect", noteGate("g1", "n1", "b1", 20000, "credit_note", user("lawyer")), 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := merged[GateKindCreditNote](context.Background(), nil, tt.g); err != nil {
				t.Fatal(err)
			}
			if woCalls != tt.woCalls || (r.notes.n["n1"].Status == noteStatusIssued) != tt.issued {
				t.Fatalf("write-off calls = %d, note = %+v", woCalls, r.notes.n["n1"])
			}
		})
	}
	if _, err := MergeEffects(notes, nil); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("merge without write-off handler err = %v", err)
	}
}
