package split

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
)

func repeat(n int, c money.Cents) []money.Cents {
	out := make([]money.Cents, n)
	for i := range out {
		out[i] = c
	}
	return out
}

func totals(rows [][]money.Cents) []money.Cents {
	if len(rows) == 0 {
		return nil
	}
	out := make([]money.Cents, len(rows[0]))
	for _, r := range rows {
		for p, c := range r {
			out[p] += c
		}
	}
	return out
}

// rule: BIL-C-090
func TestBIL_C_090_SharesSumToMillion(t *testing.T) {
	tests := []struct {
		name   string
		shares []money.PPM
		want   error
	}{
		{"60/40", []money.PPM{600000, 400000}, nil},
		{"60/39", []money.PPM{600000, 390000}, ErrSplitNot100},
		{"negative", []money.PPM{1100000, -100000}, ErrBadShare},
		{"none", nil, ErrNoPayers},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ps []Payer
			for i, s := range tt.shares {
				ps = append(ps, Payer{ID: string(rune('A' + i)), SharePPM: s, SortOrder: i})
			}
			if err := Validate(flagCtx(t, FlagJointRetainerVerified, true), ps); !errors.Is(err, tt.want) {
				t.Errorf("Validate = %v, want %v", err, tt.want)
			}
		})
	}
}

// rule: BIL-C-091
func TestBIL_C_091_CumulativeCarry(t *testing.T) {
	tests := []struct {
		name    string
		shares  []money.PPM
		amounts []money.Cents
		want    []money.Cents // per-payer totals, first payer first
		rows    [][]money.Cents
	}{
		{name: "line 10001 at 333400 first, 333300 others: others floor",
			shares: []money.PPM{333400, 333300, 333300}, amounts: []money.Cents{10001},
			want: []money.Cents{3335, 3333, 3333}},
		{name: "BIL-X-012 $1,000.01 at 60/40",
			shares: []money.PPM{600000, 400000}, amounts: []money.Cents{100001},
			want: []money.Cents{60001, 40000}},
		{name: "BIL-X-028 200 x $1.01 at 50/50",
			shares: []money.PPM{500000, 500000}, amounts: repeat(200, 101),
			want: []money.Cents{10100, 10100}},
		{name: "small lines cut from last payer and carry",
			shares: []money.PPM{100000, 450000, 450000}, amounts: []money.Cents{1, 1, 1, 1},
			want: []money.Cents{2, 1, 1},
			rows: [][]money.Cents{{1, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, 0, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := Shares(tt.shares, tt.amounts)
			if err != nil {
				t.Fatalf("Shares: %v", err)
			}
			if got := totals(rows); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("totals = %v, want %v", got, tt.want)
			}
			if tt.rows != nil && !reflect.DeepEqual(rows, tt.rows) {
				t.Errorf("rows = %v, want %v", rows, tt.rows)
			}
		})
	}
}

// rule: BIL-I-018
func TestBIL_I_018_PortionsSumToLine(t *testing.T) {
	tests := []struct {
		name    string
		shares  []money.PPM
		amounts []money.Cents
		wantErr error
	}{
		{"three payers mixed lines", []money.PPM{333334, 333333, 333333}, []money.Cents{1, 2, 0, 7, 10001, 3}, nil},
		{"tiny first share", []money.PPM{1, 499999, 500000}, []money.Cents{1, 1, 1, 1, 1, 99}, nil},
		{"negative line", []money.PPM{500000, 500000}, []money.Cents{-1}, ErrNegativeLine},
		{"overflow", []money.PPM{500000, 500000}, []money.Cents{1 << 62}, money.ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := Shares(tt.shares, tt.amounts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Shares err = %v, want %v", err, tt.wantErr)
			}
			for k, r := range rows {
				var sum money.Cents
				for _, c := range r {
					if c < 0 {
						t.Errorf("line %d: negative portion %v", k, r)
					}
					sum += c
				}
				if sum != tt.amounts[k] {
					t.Errorf("line %d: portions %v sum %d, want %d", k, r, sum, tt.amounts[k])
				}
			}
		})
	}
}

// rule: BIL-L-029
func TestBIL_L_029_JointRetainerApportioned(t *testing.T) {
	tests := []struct {
		name    string
		b       money.PPM
		want    []money.Cents
		wantErr error
	}{
		{"60/40", 400000, []money.Cents{60000, 40000}, nil},
		{"60/30 rejected", 300000, nil, ErrSplitNot100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Split{AppliesTo: All, Payers: []Payer{
				{ID: "A", SharePPM: 600000, SortOrder: 1},
				{ID: "B", SharePPM: tt.b, SortOrder: 2},
			}}
			bills, err := Apply(flagCtx(t, FlagJointRetainerVerified, true), s, []Line{{ID: "l1", Kind: Fee, Amount: 100000}})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			var got []money.Cents
			for _, b := range bills {
				got = append(got, b.Total)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("totals = %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: BIL-C-092
func TestBIL_C_092_PayerBillsIndependent(t *testing.T) {
	s := Split{AppliesTo: Fees, ClientPayerID: "client", Payers: []Payer{
		{ID: "insurer", SharePPM: 600000, SortOrder: 2},
		{ID: "client", SharePPM: 400000, SortOrder: 1},
	}}
	lines := []Line{
		{ID: "fee", Kind: Fee, Amount: 100001, SortOrder: 1},
		{ID: "disb", Kind: Disbursement, Amount: 5000, SortOrder: 2},
	}
	bills, err := Apply(flagCtx(t, FlagJointRetainerVerified, true), s, lines)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []PayerBill{
		{PayerID: "client", Total: 45001, Portions: []Portion{{"disb", 5000}, {"fee", 40001}}},
		{PayerID: "insurer", Total: 60000, Portions: []Portion{{"fee", 60000}}},
	}
	if !reflect.DeepEqual(bills, want) {
		t.Fatalf("bills = %+v, want %+v", bills, want)
	}
	tests := []struct {
		name  string
		bill  PayerBill
		trust string
		want  error
	}{
		{"own trust", bills[1], "insurer", nil},
		{"other payer's trust", bills[1], "client", ErrCrossPayerTrust},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ApplyTrust(tt.bill, tt.trust); !errors.Is(err, tt.want) {
				t.Errorf("ApplyTrust = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := Apply(flagCtx(t, FlagJointRetainerVerified, true), Split{AppliesTo: Fees, ClientPayerID: "nobody", Payers: s.Payers}, lines); !errors.Is(err, ErrNoClientPayer) {
		t.Errorf("Apply without client payer = %v, want %v", err, ErrNoClientPayer)
	}
}

// rule: BIL-C-111
func TestBIL_C_111_CombinedBillTrustPerMatter(t *testing.T) {
	c := Combine([]Line{
		{ID: "b1", MatterID: "B", Amount: 2000, SortOrder: 2},
		{ID: "a1", MatterID: "A", Amount: 100000, SortOrder: 1},
		{ID: "a2", MatterID: "A", Amount: 500, SortOrder: 3},
	})
	if c.Total != 102500 || len(c.Matters) != 2 || c.Matters[0].MatterID != "A" ||
		c.Matters[0].Subtotal != 100500 || c.Matters[1].Subtotal != 2000 {
		t.Fatalf("Combine = %+v", c)
	}
	tests := []struct {
		name, trust, line string
		want              money.Cents
		wantErr           error
	}{
		{"matter B trust cannot pay A lines", "B", "A", 0, ErrCrossMatterTrust},
		{"matter A trust capped at A subtotal", "A", "A", 100500, nil},
		{"matter B trust capped at B subtotal", "B", "B", 2000, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.TrustCap(tt.trust, tt.line)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Errorf("TrustCap = %d, %v; want %d, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// flagCtx returns a context whose flag set has name defined, and turned on when on.
func flagCtx(t *testing.T, name string, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: name, Owner: "test", Expiry: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(name, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// rule: BIL-L-029 (verify)
func TestBIL_L_029_FlagOffRefusesSplit(t *testing.T) {
	valid := []Payer{{ID: "A", SharePPM: 600000, SortOrder: 1}, {ID: "B", SharePPM: 400000, SortOrder: 2}}
	tests := []struct {
		name   string
		on     bool
		payers []Payer
		want   error
	}{
		{"off refuses valid split", false, valid, ErrUnverified},
		{"off still refuses 60/30", false, []Payer{{ID: "A", SharePPM: 600000}, {ID: "B", SharePPM: 300000}}, ErrSplitNot100},
		{"off still refuses no payers", false, nil, ErrNoPayers},
		{"on allows valid split", true, valid, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := flagCtx(t, FlagJointRetainerVerified, tt.on)
			if err := Validate(ctx, tt.payers); !errors.Is(err, tt.want) {
				t.Fatalf("Validate = %v, want %v", err, tt.want)
			}
			bills, err := Apply(ctx, Split{AppliesTo: All, Payers: tt.payers}, []Line{{ID: "l1", Kind: Fee, Amount: 100000}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Apply = %v, want %v", err, tt.want)
			}
			if tt.want != nil && bills != nil {
				t.Fatalf("Apply returned bills %+v with error", bills)
			}
		})
	}
	if err := Validate(context.Background(), valid); !errors.Is(err, ErrUnverified) {
		t.Fatalf("no flag set: Validate = %v, want ErrUnverified", err)
	}
}
