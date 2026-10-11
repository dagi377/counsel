package trust

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
)

// cashMem adds the cash source to memRepo: trust cash receipts come from the posted
// transactions, operating cash from a per-matter map.
type cashMem struct {
	*memRepo
	operating map[string]int64
	gates     map[string]CashGate // approved trust.cash_over_limit gates by id
}

func (c cashMem) approvedCashGate(_ context.Context, _ pgx.Tx, id string) (CashGate, error) {
	g, ok := c.gates[id]
	if !ok {
		return CashGate{}, ErrNotFound
	}
	return g, nil
}

func (c cashMem) lockMatterCash(_ context.Context, _ pgx.Tx, matterID string) error {
	c.ops = append(c.ops, "lockcash:"+matterID)
	return nil
}

func (c cashMem) cashReceivedCents(_ context.Context, _ pgx.Tx, matterID string) (int64, error) {
	total := c.operating[matterID]
	for _, t := range c.posted {
		if t.Method == MethodCash && t.AmountCents > 0 && c.ledgers[t.LedgerID].MatterID == matterID {
			total += t.AmountCents
		}
	}
	return total, nil
}

func cashReceipt(l LedgerID, cents int64, code string) Receipt {
	r := receipt(l, cents)
	r.Method, r.CashExceptionCode = MethodCash, code
	if code != "" {
		r.CashGateID = "g6c-1"
	}
	return r
}

// approvedG6c is the approved G6c gate "g6c-1" for matter A, 250000 cents, financial institution.
var approvedG6c = map[string]CashGate{"g6c-1": {MatterID: "A", AmountCents: 250000, Exception: CashFromFinancialInstitution}}

// rule: BIL-L-039
func TestBIL_L_039_CashLimit(t *testing.T) {
	tests := []struct {
		name      string
		operating int64 // prior cash fee payments into operating
		trust     int64 // prior trust cash receipt, 0 for none
		amount    int64
		code      string
		on        bool
		wantErr   error
		wantTotal int64
		wantWarn  bool
	}{
		{"operating 500000 + trust 249999 = 749999 allowed", 500000, 0, 249999, "", true, nil, 749999, true},
		{"operating 500000 + trust 250000 = 750000 refused", 500000, 0, 250000, "", true, ErrCashLimit, 750000, true},
		{"trust 500000 + trust 250000 = 750000 refused", 0, 500000, 250000, "", true, ErrCashLimit, 750000, true},
		{"750000 with financial institution exception and approved G6c gate allowed", 500000, 0, 250000, CashFromFinancialInstitution, true, nil, 750000, true},
		{"unknown exception code refused", 500000, 0, 250000, "client_asked", true, ErrInvalid, 0, false},
		{"590000 + 10000 = 600000 allowed with warning", 590000, 0, 10000, "", true, nil, 600000, true},
		{"589999 + 10000 = 599999 no warning", 589999, 0, 10000, "", true, nil, 599999, false},
		{"flag off: cash refused, fails closed", 0, 0, 100, "", false, ErrUnverified, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			repo := cashMem{memRepo: r.repo, operating: map[string]int64{"A": tt.operating}, gates: approvedG6c}
			r.s.repo = repo
			ctx := legalflagstest.Context(t, FlagBylaw9Verified, tt.on)
			l := r.open(t, "acct-a", "A")
			if tt.trust > 0 {
				if _, err := r.s.Receive(ctx, nil, cashReceipt(l, tt.trust, "")); err != nil {
					t.Fatalf("prior receipt: %v", err)
				}
			}
			if tt.code == "" && (tt.wantErr == nil || errors.Is(tt.wantErr, ErrCashLimit)) {
				c, err := r.s.CheckCash(ctx, "A", tt.amount)
				if err != nil {
					t.Fatalf("check cash: %v", err)
				}
				if c.TotalCents != tt.wantTotal || c.Warn != tt.wantWarn || c.Blocked != errors.Is(tt.wantErr, ErrCashLimit) {
					t.Fatalf("check cash = %+v, want total %d warn %v", c, tt.wantTotal, tt.wantWarn)
				}
			}
			before := len(r.repo.posted)
			_, err := r.s.Receive(ctx, nil, cashReceipt(l, tt.amount, tt.code))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("receive err = %v, want %v", err, tt.wantErr)
			}
			if posted := len(r.repo.posted) - before; (tt.wantErr == nil) != (posted == 1) {
				t.Fatalf("posted %d transactions, want error %v", posted, tt.wantErr)
			}
		})
	}
}

// rule: BIL-L-039
func TestBIL_L_039_OnlyCashIsCounted(t *testing.T) {
	tests := []struct {
		name   string
		method Method
	}{
		{"eft over the limit, flag off", MethodEFT},
		{"wire over the limit, flag off", MethodWire},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.s.repo = cashMem{memRepo: r.repo, operating: map[string]int64{"A": 900000}}
			l := r.open(t, "acct-a", "A")
			rc := receipt(l, 900000)
			rc.Method = tt.method
			if _, err := r.s.Receive(legalflagstest.Context(t, FlagBylaw9Verified, false), nil, rc); err != nil {
				t.Fatalf("receive: %v", err)
			}
		})
	}
}

// rule: BIL-L-039
func TestBIL_L_039_CheckCashFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		on      bool
		matter  string
		amount  int64
		wantErr error
	}{
		{"flag off", false, "A", 100, ErrUnverified},
		{"no matter", true, "", 100, ErrInvalid},
		{"zero amount", true, "A", 0, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.s.repo = cashMem{memRepo: r.repo, operating: map[string]int64{}}
			if _, err := r.s.CheckCash(legalflagstest.Context(t, FlagBylaw9Verified, tt.on), tt.matter, tt.amount); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: BIL-L-039
func TestBIL_L_039_EvaluateCashBoundaries(t *testing.T) {
	tests := []struct {
		prior, amount int64
		warn, blocked bool
	}{
		{0, 599999, false, false},
		{0, 600000, true, false},
		{0, 749999, true, false},
		{0, 750000, true, true},
		{749999, 1, true, true},
	}
	for _, tt := range tests {
		c := EvaluateCash(tt.prior, tt.amount)
		if c.TotalCents != tt.prior+tt.amount || c.Warn != tt.warn || c.Blocked != tt.blocked {
			t.Errorf("EvaluateCash(%d, %d) = %+v", tt.prior, tt.amount, c)
		}
	}
}

// rule: BIL-L-039
func TestBIL_L_039_CashExceptionNeedsApprovedGate(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		gateID  string
		gates   map[string]CashGate
		wantErr error
	}{
		{"code with approved gate allowed", CashFromFinancialInstitution, "g6c-1", approvedG6c, nil},
		{"code without gate refused", CashFromFinancialInstitution, "", approvedG6c, ErrCashGateRequired},
		{"code with unapproved gate refused", CashFromFinancialInstitution, "g6c-1", nil, ErrCashGateRequired},
		{"gate for another code refused", CashForBail, "g6c-1", approvedG6c, ErrCashGateRequired},
		{"gate for another amount refused", CashFromFinancialInstitution, "g6c-2",
			map[string]CashGate{"g6c-2": {MatterID: "A", AmountCents: 100, Exception: CashFromFinancialInstitution}}, ErrCashGateRequired},
		{"gate for another matter refused", CashFromFinancialInstitution, "g6c-3",
			map[string]CashGate{"g6c-3": {MatterID: "B", AmountCents: 250000, Exception: CashFromFinancialInstitution}}, ErrCashGateRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.s.repo = cashMem{memRepo: r.repo, operating: map[string]int64{"A": 500000}, gates: tt.gates}
			ctx := legalflagstest.Context(t, FlagBylaw9Verified, true)
			l := r.open(t, "acct-a", "A")
			rc := cashReceipt(l, 250000, tt.code)
			rc.CashGateID = tt.gateID
			before := len(r.repo.posted)
			_, err := r.s.Receive(ctx, nil, rc)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("receive err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && !errors.Is(err, ErrCashLimit) {
				t.Fatalf("receive err = %v, want it to also be ErrCashLimit", err)
			}
			if posted := len(r.repo.posted) - before; (tt.wantErr == nil) != (posted == 1) {
				t.Fatalf("posted %d transactions, want error %v", posted, tt.wantErr)
			}
		})
	}
}

// rule: BIL-L-039
func TestBIL_L_039_OperatingCashPathEnforcesLimit(t *testing.T) {
	tests := []struct {
		name      string
		on        bool
		operating int64
		trust     int64
		amount    int64
		code      string
		gateID    string
		wantErr   error
	}{
		{"under the limit allowed", true, 0, 0, 749999, "", "", nil},
		{"operating reaches 750000 refused", true, 500000, 0, 250000, "", "", ErrCashLimit},
		{"trust cash counts toward the operating limit", true, 0, 500000, 250000, "", "", ErrCashLimit},
		{"exception code without gate refused", true, 500000, 0, 250000, CashFromFinancialInstitution, "", ErrCashGateRequired},
		{"exception code with approved gate allowed", true, 500000, 0, 250000, CashFromFinancialInstitution, "g6c-1", nil},
		{"unknown exception code refused", true, 500000, 0, 250000, "client_asked", "g6c-1", ErrInvalid},
		{"flag off fails closed", false, 0, 0, 100, "", "", ErrUnverified},
		{"no matter refused", true, 0, 0, 100, "", "", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			repo := cashMem{memRepo: r.repo, operating: map[string]int64{"A": tt.operating}, gates: approvedG6c}
			r.s.repo = repo
			l := r.open(t, "acct-a", "A")
			if tt.trust > 0 {
				on := legalflagstest.Context(t, FlagBylaw9Verified, true)
				if _, err := r.s.Receive(on, nil, cashReceipt(l, tt.trust, "")); err != nil {
					t.Fatalf("prior receipt: %v", err)
				}
			}
			matter := "A"
			if tt.name == "no matter refused" {
				matter = ""
			}
			ctx := legalflagstest.Context(t, FlagBylaw9Verified, tt.on)
			err := r.s.CheckOperatingCash(ctx, nil, matter, tt.amount, tt.code, tt.gateID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil || errors.Is(tt.wantErr, ErrCashLimit) || errors.Is(tt.wantErr, ErrCashGateRequired) {
				if !slices.Contains(repo.ops, "lockcash:A") {
					t.Fatalf("operating cash did not take the per-matter cash lock: %v", repo.ops)
				}
			}
		})
	}
}
