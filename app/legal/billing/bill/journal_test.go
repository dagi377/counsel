package bill

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

func hstON(base, tax int64) TaxLine {
	return TaxLine{TaxCode: "HST-ON", Component: "HST", RatePPM: 130000, BaseCents: base, TaxCents: tax}
}

// recordingLedger is a ledgerfake that also finds the approval entry by source, as
// PGStore.ApprovalEntry does over gl.journal_entry.
type recordingLedger struct {
	*ledgerfake.Fake
	posted map[string]ledger.EntryID // source id -> entry
}

func newRecordingLedger() *recordingLedger {
	return &recordingLedger{Fake: ledgerfake.New(ledger.Date{Year: 2026, Month: 10, Day: 8}), posted: map[string]ledger.EntryID{}}
}

func (r *recordingLedger) Post(ctx context.Context, tx pgx.Tx, e ledger.Entry) (ledger.EntryID, error) {
	id, err := r.Fake.Post(ctx, tx, e)
	if err == nil && e.SourceKind == SourceBill {
		r.posted[e.SourceID] = id
	}
	return id, err
}

func (r *recordingLedger) ApprovalEntry(_ context.Context, _ pgx.Tx, id ID) (ledger.EntryID, error) {
	e, ok := r.posted[string(id)]
	if !ok {
		return "", ledger.ErrNotFound
	}
	return e, nil
}

// approvedBill is an approved Ontario bill of BIL-X-003: $4,212.50 fees in two lines, $86.40
// taxable and $229.00 agency disbursements, HST $558.86.
func approvedBill() (Bill, []TaxLine) {
	b := Bill{
		ID: "b1", Number: "INV-2026-0001", MatterID: "m1", PayerID: "p1", Status: StatusApproved,
		IssueDate: time.Date(2026, 10, 6, 0, 0, 0, 0, business),
		Lines: []Line{
			{Ordinal: 1, Source: SourceTime, AmountCents: 300000, TaxCode: "HST-ON"},
			{Ordinal: 2, Source: SourceFlat, AmountCents: 121250, TaxCode: "HST-ON"},
			{Ordinal: 3, Source: SourceExpense, AmountCents: 8640, TaxCode: "HST-ON"},
			{Ordinal: 4, Source: SourceExpense, AmountCents: 22900, TaxCode: "EXEMPT"},
		},
		FeesCents: 421250, TaxableDisbCents: 8640, NontaxableDisbCents: 22900, TaxCents: 55886,
		TotalCents: 508676, PlaceOfSupplyProvince: "ON",
	}
	return b, []TaxLine{hstON(429890, 55886)}
}

func netByAccount(lines []ledger.Line) map[string]int64 {
	net := map[string]int64{}
	for _, l := range lines {
		if l.Side == ledger.Debit {
			net[l.Account] += int64(l.Amount)
		} else {
			net[l.Account] -= int64(l.Amount)
		}
	}
	return net
}

// rule: BIL-J-003
func TestBIL_J_003_ApprovalJournal(t *testing.T) {
	with := func(f func(*Bill, *[]TaxLine)) (Bill, []TaxLine) {
		b, tx := approvedBill()
		f(&b, &tx)
		return b, tx
	}
	tests := []struct {
		name  string
		build func() (Bill, []TaxLine)
		net   map[string]int64 // debit minus credit per account
		lines int
		err   error
	}{
		{"BIL-X-003 Ontario bill", approvedBill, map[string]int64{
			books.AcctARFees: 508676, books.AcctFeeRevenue: -421250, books.AcctCostRecoveryRevenue: -8640,
			books.AcctDisbRecoverable: -22900, books.AcctSalesTaxPayable: -55886,
		}, 6, nil},
		{"discount and negative adjustment hit fee adjustments", func() (Bill, []TaxLine) {
			return with(func(b *Bill, tx *[]TaxLine) {
				b.Lines = append(b.Lines, Line{Ordinal: 5, Source: SourceAdjustment, AmountCents: -21250})
				b.FeesCents -= 21250
				b.DiscountCents = 10000
				*tx = []TaxLine{hstON(398640, 51824)}
				b.TaxCents = 51824
				b.TotalCents = b.FeesCents + b.TaxableDisbCents + b.NontaxableDisbCents - b.DiscountCents + b.TaxCents
			})
		}, map[string]int64{
			books.AcctARFees: 473364, books.AcctFeeAdjustments: 31250, books.AcctFeeRevenue: -421250,
			books.AcctCostRecoveryRevenue: -8640, books.AcctDisbRecoverable: -22900, books.AcctSalesTaxPayable: -51824,
		}, 8, nil},
		{"GST and PST split across payable accounts", func() (Bill, []TaxLine) {
			return with(func(b *Bill, tx *[]TaxLine) {
				*tx = []TaxLine{
					{TaxCode: "GST", Component: "GST", RatePPM: 50000, BaseCents: 429890, TaxCents: 21495},
					{TaxCode: "PST-BC", Component: "PST", RatePPM: 70000, BaseCents: 429890, TaxCents: 30093},
				}
				b.TaxCents = 51588
				b.TotalCents = 421250 + 8640 + 22900 + 51588
			})
		}, map[string]int64{
			books.AcctARFees: 504378, books.AcctFeeRevenue: -421250, books.AcctCostRecoveryRevenue: -8640,
			books.AcctDisbRecoverable: -22900, books.AcctSalesTaxPayable: -21495, books.AcctProvincialTax: -30093,
		}, 7, nil},
		{"zero-rated tax line posts nothing", func() (Bill, []TaxLine) {
			return with(func(b *Bill, tx *[]TaxLine) {
				*tx = append(*tx, TaxLine{TaxCode: "ZERO", Component: "NONE"})
			})
		}, map[string]int64{
			books.AcctARFees: 508676, books.AcctFeeRevenue: -421250, books.AcctCostRecoveryRevenue: -8640,
			books.AcctDisbRecoverable: -22900, books.AcctSalesTaxPayable: -55886,
		}, 6, nil},
		{"tax lines differ from bill tax", func() (Bill, []TaxLine) {
			return with(func(_ *Bill, tx *[]TaxLine) { *tx = []TaxLine{hstON(429890, 55885)} })
		}, nil, 0, ErrInvalid},
		{"missing tax breakdown", func() (Bill, []TaxLine) {
			return with(func(_ *Bill, tx *[]TaxLine) { *tx = nil })
		}, nil, 0, ErrInvalid},
		{"unknown tax component", func() (Bill, []TaxLine) {
			return with(func(_ *Bill, tx *[]TaxLine) { (*tx)[0].Component = "VAT" })
		}, nil, 0, ErrInvalid},
		{"fee lines differ from bill fees", func() (Bill, []TaxLine) {
			return with(func(b *Bill, _ *[]TaxLine) { b.Lines[0].AmountCents-- })
		}, nil, 0, ErrInvalid},
		{"disbursement lines differ from bill", func() (Bill, []TaxLine) {
			return with(func(b *Bill, _ *[]TaxLine) { b.Lines = b.Lines[:3] })
		}, nil, 0, ErrInvalid},
		{"total does not add up", func() (Bill, []TaxLine) {
			return with(func(b *Bill, _ *[]TaxLine) { b.TotalCents++ })
		}, nil, 0, ErrInvalid},
		{"no bill date", func() (Bill, []TaxLine) {
			return with(func(b *Bill, _ *[]TaxLine) { b.IssueDate = time.Time{} })
		}, nil, 0, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, taxes := tc.build()
			e, err := ApprovalJournal(b, taxes)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err != nil {
				return
			}
			if e.Book != books.BookOperating || e.SourceKind != SourceBill || e.SourceID != "b1" ||
				e.Date != (clock.Date{Year: 2026, Month: 10, Day: 6}) {
				t.Fatalf("entry header = %+v", e)
			}
			if err := ledger.Validate(e); err != nil {
				t.Fatalf("entry does not balance: %v", err)
			}
			if len(e.Lines) != tc.lines {
				t.Fatalf("entry has %d lines, want %d: %+v", len(e.Lines), tc.lines, e.Lines)
			}
			var sum int64
			got := netByAccount(e.Lines)
			for _, v := range got {
				sum += v
			}
			if sum != 0 {
				t.Fatalf("debits minus credits = %d", sum)
			}
			for acct, want := range tc.net {
				if got[acct] != want {
					t.Errorf("%s nets %d, want %d", acct, got[acct], want)
				}
			}
			if len(got) != len(tc.net) {
				t.Errorf("accounts = %v, want %v", got, tc.net)
			}
		})
	}
}

// rule: BIL-J-003
func TestTaxPayableAccount(t *testing.T) {
	tests := []struct {
		component string
		want      string
		err       error
	}{
		{"HST", books.AcctSalesTaxPayable, nil},
		{"GST", books.AcctSalesTaxPayable, nil},
		{"PST", books.AcctProvincialTax, nil},
		{"QST", books.AcctProvincialTax, nil},
		{"NONE", "", ErrInvalid},
		{"", "", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.component, func(t *testing.T) {
			got, err := TaxPayableAccount(tc.component)
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("TaxPayableAccount(%q) = %q, %v; want %q, %v", tc.component, got, err, tc.want, tc.err)
			}
		})
	}
	// Every tax line credits its own payable account, one line per tax line.
	b, _ := approvedBill()
	taxes := []TaxLine{
		{TaxCode: "GST", Component: "GST", TaxCents: 21495},
		{TaxCode: "PST-BC", Component: "PST", TaxCents: 30093},
	}
	b.TaxCents, b.TotalCents = 51588, 421250+8640+22900+51588
	e, err := ApprovalJournal(b, taxes)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range taxes {
		want, _ := TaxPayableAccount(tl.Component)
		found := 0
		for _, l := range e.Lines {
			if l.Dimensions["tax_code"] == tl.TaxCode {
				found++
				if l.Account != want || l.Side != ledger.Credit || int64(l.Amount) != tl.TaxCents {
					t.Errorf("tax line %s posted %+v, want Cr %s %d", tl.TaxCode, l, want, tl.TaxCents)
				}
			}
		}
		if found != 1 {
			t.Errorf("tax line %s posted %d times", tl.TaxCode, found)
		}
	}
}

// rule: BIL-J-003
// rule: BIL-C-084
func TestApproveThenVoidNetsToZero(t *testing.T) {
	approveAt := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	voidAt := time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	tests := []struct {
		name  string
		build func() (Bill, []TaxLine)
	}{
		{"HST bill with disbursements", approvedBill},
		{"GST and PST with a discount", func() (Bill, []TaxLine) {
			b, _ := approvedBill()
			b.DiscountCents = 5000
			taxes := []TaxLine{{TaxCode: "GST", Component: "GST", TaxCents: 21245}, {TaxCode: "PST-BC", Component: "PST", TaxCents: 29743}}
			b.TaxCents = 50988
			b.TotalCents = b.FeesCents + b.TaxableDisbCents + b.NontaxableDisbCents - b.DiscountCents + b.TaxCents
			return b, taxes
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			b, taxes := tc.build()
			b.Status, b.Number, b.IssueDate = StatusPendingApproval, "", time.Time{}
			b.PreparedBy = "u1"
			store := &memStore{bills: map[ID]Bill{b.ID: b}, taxes: map[ID][]TaxLine{b.ID: taxes}}
			led := newRecordingLedger()
			var book memFeesBook
			approve := &ApproveEffect{Store: store, FeesBook: &book, Journal: store, Ledger: led, Prefix: "INV",
				Now: func() time.Time { return approveAt },
				Next: func(_ context.Context, _ pgx.Tx, prefix string, year int) (string, error) {
					return FormatNumber(prefix, year, 1)
				}}
			payload, _ := json.Marshal(ApprovalPayload{BillID: string(b.ID), MatterID: b.MatterID, TotalCents: b.TotalCents})
			if err := approve.Apply(ctx, nil, registry.Approved{GateID: "g-approve", Kind: GateBillApproval, Payload: payload,
				Preparer: user("u1"), Approvers: []registry.Principal{user("u2")}}); err != nil {
				t.Fatalf("approve: %v", err)
			}
			accounts := []string{books.AcctARFees, books.AcctFeeAdjustments, books.AcctFeeRevenue, books.AcctCostRecoveryRevenue,
				books.AcctDisbRecoverable, books.AcctSalesTaxPayable, books.AcctProvincialTax}
			asOf := clock.DateOf(voidAt, business)
			ar, _ := led.Balance(ctx, books.AcctARFees, asOf)
			if int64(ar) != b.TotalCents {
				t.Fatalf("AR after approval = %d, want %d", ar, b.TotalCents)
			}

			vstore := &voidMemStore{bills: map[ID]Bill{b.ID: store.bills[b.ID]}}
			void := &VoidEffect{Store: vstore, Entries: led, Ledger: led, Now: func() time.Time { return voidAt }}
			vpayload, _ := json.Marshal(VoidPayload{BillID: string(b.ID), Reason: "duplicate"})
			if err := void.Apply(ctx, nil, registry.Approved{GateID: "g-void", Kind: GateBillVoid, Payload: vpayload,
				Preparer: user("u1"), Approvers: []registry.Principal{user("u2")}}); err != nil {
				t.Fatalf("void: %v", err)
			}
			for _, acct := range accounts {
				bal, err := led.Balance(ctx, acct, asOf)
				if err != nil {
					t.Fatal(err)
				}
				if bal != 0 {
					t.Errorf("%s nets %d after approve and void, want 0", acct, bal)
				}
			}
		})
	}
}
