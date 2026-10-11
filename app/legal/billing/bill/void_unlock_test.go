package bill_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// fakeVoidStore is a bill.VoidStore over billfake.
type fakeVoidStore struct{ bills *billfake.Fake }

func (s fakeVoidStore) Lock(ctx context.Context, _ pgx.Tx, id bill.ID) (bill.Bill, error) {
	return s.bills.Get(ctx, id)
}

func (s fakeVoidStore) SaveVoid(_ context.Context, _ pgx.Tx, b bill.Bill) error {
	return s.bills.Put(b)
}

type oneEntry struct{}

func (oneEntry) ApprovalEntry(context.Context, pgx.Tx, bill.ID) (ledger.EntryID, error) {
	return "je-approval", nil
}

type nopReverser struct{}

func (nopReverser) Reverse(_ context.Context, _ pgx.Tx, id ledger.EntryID, _ string) (ledger.EntryID, error) {
	return "rev-" + id, nil
}

// billedEntry records an entry for timekeeper and moves it to billed.
func billedEntry(t *testing.T, times *timekeepingfake.Fake) tk.EntryID {
	t.Helper()
	ctx := context.Background()
	id, err := times.Record(ctx, nil, tk.TimeEntry{MatterID: "m-1", UserID: "u-tk", Minutes: 60, Billable: true,
		WorkDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}, tk.Actor{UserID: "u-tk"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		to tk.State
		by string
	}{{tk.StateSubmitted, "u-tk"}, {tk.StateApproved, "u-lawyer"}, {tk.StateOnDraftBill, "u-prep"}, {tk.StateBilled, "u-lawyer"}} {
		if err := times.Transition(ctx, nil, id, step.to, tk.Actor{UserID: step.by}); err != nil {
			t.Fatalf("entry %s -> %s: %v", id, step.to, err)
		}
	}
	return id
}

func approvedBill(id bill.ID, number string, entries ...tk.EntryID) bill.Bill {
	b := bill.Bill{ID: id, Number: number, MatterID: "m-1", Status: bill.StatusApproved, PreparedBy: "u-prep",
		IssueDate: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), ApprovalID: "g-approve-" + string(id), ApprovedBy: "u-lawyer"}
	for i, e := range entries {
		b.Lines = append(b.Lines, bill.Line{Ordinal: i + 1, Source: bill.SourceTime, SourceID: string(e), AmountCents: 50000})
		b.FeesCents += 50000
	}
	b.TaxCents = b.FeesCents * 13 / 100
	b.TotalCents = b.FeesCents + b.TaxCents
	return b
}

func TestBIL_C_084_VoidFreesEntriesForRebilling(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	times := timekeepingfake.New(nil, nil)
	bills := billfake.New(func() time.Time { return now })
	box := outboxfake.New()

	voided := []tk.EntryID{billedEntry(t, times), billedEntry(t, times)}
	kept := billedEntry(t, times)
	for _, b := range []bill.Bill{approvedBill("b-1", "INV-2026-0141", voided...), approvedBill("b-2", "INV-2026-0142", kept)} {
		if err := bills.Put(b); err != nil {
			t.Fatal(err)
		}
	}

	eff := &bill.VoidEffect{Store: fakeVoidStore{bills}, Entries: oneEntry{}, Ledger: nopReverser{},
		Sink: bill.VoidPublisher{Outbox: box}, Now: func() time.Time { return now }}
	payload, err := json.Marshal(bill.VoidPayload{BillID: "b-1", Reason: "duplicate"})
	if err != nil {
		t.Fatal(err)
	}
	gate := registry.Approved{GateID: "g-void", Kind: bill.GateBillVoid, Payload: payload,
		Preparer:  registry.Principal{Kind: registry.PrincipalUser, UserID: "u-clerk"},
		Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: "u-lawyer"}}}
	if err := eff.Apply(ctx, nil, gate); err != nil {
		t.Fatalf("void: %v", err)
	}

	// rule: BIL-C-084
	// The void emits bill.voided with the bill's entry ids, ids only.
	msgs := box.Messages()
	if len(msgs) != 1 || msgs[0].Type != bill.EventBillVoided || msgs[0].Subject != (outbox.Subject{Kind: "bill", ID: "b-1"}) {
		t.Fatalf("outbox = %+v, want one bill.voided for b-1", msgs)
	}
	var ev bill.VoidedEvent
	if err := json.Unmarshal(msgs[0].Payload, &ev); err != nil {
		t.Fatal(err)
	}
	want := []string{string(voided[0]), string(voided[1])}
	if ev.BillID != "b-1" || ev.GateID != "g-void" || !slices.Equal(ev.TimeEntryIDs, want) {
		t.Fatalf("event = %+v, want entries %v", ev, want)
	}

	// The time module's port returns them to approved.
	if err := bill.ReleaseVoidedEntries(ctx, nil, times, ev, tk.Actor{UserID: "u-lawyer"}); err != nil {
		t.Fatalf("release: %v", err)
	}

	b1, err := bills.Get(ctx, "b-1")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := bills.Get(ctx, "b-2")
	if err != nil {
		t.Fatal(err)
	}
	if b1.Status != bill.StatusVoid || b1.Number != "INV-2026-0141" {
		t.Fatalf("voided bill = %s %q, want void with its number", b1.Status, b1.Number)
	}
	all := []bill.Bill{b1, b2}

	tests := []struct {
		name  string
		entry tk.EntryID
		err   error
	}{
		{name: "first entry of the voided bill goes on a new draft", entry: voided[0]},
		{name: "second entry of the voided bill goes on a new draft", entry: voided[1]},
		{name: "entry of a non-void bill stays locked", entry: kept, err: tk.ErrBilledLocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := times.Get(ctx, tt.entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := bill.CheckTimeEntryFree(e, all); !errors.Is(err, tt.err) {
				t.Fatalf("CheckTimeEntryFree = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestBIL_C_084_ReleaseVoidedEntries(t *testing.T) {
	ctx := context.Background()
	times := timekeepingfake.New(nil, nil)
	billed := billedEntry(t, times)
	tests := []struct {
		name string
		ids  []string
		err  error
	}{
		{name: "no entries is a no-op", ids: nil},
		{name: "billed entry returns to approved", ids: []string{string(billed)}},
		{name: "unknown entry fails", ids: []string{"te-missing"}, err: tk.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bill.ReleaseVoidedEntries(ctx, nil, times, bill.VoidedEvent{BillID: "b-1", TimeEntryIDs: tt.ids}, tk.Actor{UserID: "u-lawyer"})
			if !errors.Is(err, tt.err) {
				t.Fatalf("release = %v, want %v", err, tt.err)
			}
		})
	}
	e, err := times.Get(ctx, billed)
	if err != nil {
		t.Fatal(err)
	}
	if e.State != tk.StateApproved {
		t.Fatalf("entry state = %s, want approved", e.State)
	}
}

func TestBIL_C_084_CheckTimeEntryFree(t *testing.T) {
	entry := tk.TimeEntry{ID: "te-1", State: tk.StateApproved}
	onBill := func(status bill.Status) bill.Bill {
		return bill.Bill{ID: "b-9", Status: status, Lines: []bill.Line{{Source: bill.SourceTime, SourceID: "te-1"}}}
	}
	tests := []struct {
		name  string
		entry tk.TimeEntry
		bills []bill.Bill
		err   error
	}{
		{name: "on no bill", entry: entry},
		{name: "only on a void bill", entry: entry, bills: []bill.Bill{onBill(bill.StatusVoid)}},
		{name: "on a draft bill", entry: entry, bills: []bill.Bill{onBill(bill.StatusDraft)}, err: tk.ErrBilledLocked},
		{name: "on a delivered bill", entry: entry, bills: []bill.Bill{onBill(bill.StatusDelivered)}, err: tk.ErrBilledLocked},
		{name: "void and live bill", entry: entry, bills: []bill.Bill{onBill(bill.StatusVoid), onBill(bill.StatusApproved)}, err: tk.ErrBilledLocked},
		{name: "expense with the same id does not lock", entry: entry,
			bills: []bill.Bill{{ID: "b-8", Status: bill.StatusApproved, Lines: []bill.Line{{Source: bill.SourceExpense, SourceID: "te-1"}}}}},
		{name: "not yet released", entry: tk.TimeEntry{ID: "te-1", State: tk.StateBilled}, err: tk.ErrBadTransition},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := bill.CheckTimeEntryFree(tt.entry, tt.bills); !errors.Is(err, tt.err) {
				t.Fatalf("CheckTimeEntryFree = %v, want %v", err, tt.err)
			}
		})
	}
}
