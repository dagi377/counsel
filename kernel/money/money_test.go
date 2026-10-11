package money

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// rule: BIL-C-001
func TestBIL_C_001_TaxOnCentsAtPPM(t *testing.T) {
	tests := []struct {
		name string
		c    Cents
		rate PPM
		want Cents
	}{
		{name: "9.975% of 2000.00", c: 200000, rate: 99750, want: 19950},
		{name: "13% of 10.00", c: 1000, rate: 130000, want: 130},
		{name: "10% discount stored as 100000", c: 5005, rate: 100000, want: 501},
		{name: "negative", c: -5005, rate: 100000, want: -501},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyPPM(tt.c, tt.rate)
			if err != nil || got != tt.want {
				t.Fatalf("ApplyPPM(%d, %d) = %d, %v; want %d", tt.c, tt.rate, got, err, tt.want)
			}
		})
	}
	if got, err := Rhu(200000*99750, 1e6); err != nil || got != 19950 {
		t.Fatalf("rhu(200000*99750, 1e6) = %d, %v; want 19950", got, err)
	}
}

// rule: BIL-C-002
func TestBIL_C_002_RhuHalfAwayFromZero(t *testing.T) {
	tests := []struct {
		name     string
		num, den int64
		want     int64
		err      error
	}{
		{name: "half up", num: 5, den: 10, want: 1},
		{name: "half away negative", num: -5, den: 10, want: -1},
		{name: "below half", num: 4, den: 10, want: 0},
		{name: "below half negative", num: -4, den: 10, want: 0},
		{name: "55885.7", num: 558857, den: 10, want: 55886},
		{name: "65.5", num: 655, den: 10, want: 66},
		{name: "-65.5", num: -655, den: 10, want: -66},
		{name: "negative denominator", num: 5, den: -10, want: -1},
		{name: "both negative", num: -15, den: -10, want: 2},
		{name: "exact", num: 100, den: 4, want: 25},
		{name: "min int exact", num: math.MinInt64, den: 1, want: math.MinInt64},
		{name: "max int / 2", num: math.MaxInt64, den: 2, want: math.MaxInt64/2 + 1},
		{name: "divide by zero", num: 1, den: 0, err: ErrDivideByZero},
		{name: "overflow", num: math.MinInt64, den: -1, err: ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Rhu(tt.num, tt.den)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Rhu(%d, %d) err = %v, want %v", tt.num, tt.den, err, tt.err)
			}
			if err == nil && got != tt.want {
				t.Fatalf("Rhu(%d, %d) = %d, want %d", tt.num, tt.den, got, tt.want)
			}
		})
	}
	// Round once on the summed base, never per line: 333/333/334 at 13% is 130, not 129.
	perLine := int64(0)
	for _, l := range []int64{333, 333, 334} {
		v, _ := MulRhu(l, 130000, PPMScale)
		perLine += v
	}
	once, _ := MulRhu(1000, 130000, PPMScale)
	if once != 130 || perLine != 129 {
		t.Fatalf("summed base = %d (want 130), per line = %d (want 129)", once, perLine)
	}
}

// bigRhu is the reference: num/den rounded half away from zero with big.Rat.
func bigRhu(num, den *big.Int) *big.Int {
	r := new(big.Rat).SetFrac(num, den)
	abs := new(big.Rat).Abs(r)
	abs.Add(abs, big.NewRat(1, 2))
	q := new(big.Int).Quo(abs.Num(), abs.Denom())
	if r.Sign() < 0 {
		q.Neg(q)
	}
	return q
}

func TestRhuProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	limit := new(big.Int).Lsh(big.NewInt(1), 62)
	for i := 0; i < 20000; i++ {
		a := rng.Int63n(1<<31) - 1<<30
		b := rng.Int63n(1<<31) - 1<<30
		d := rng.Int63n(1<<20) - 1<<19
		if i%3 == 0 {
			d = []int64{2, 10, 100, 1_000_000, -7}[i%5]
		}
		if d == 0 {
			continue
		}
		p := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
		if new(big.Int).Abs(p).Cmp(limit) >= 0 {
			continue
		}
		want := bigRhu(p, big.NewInt(d))
		got, err := MulRhu(a, b, d)
		if err != nil || got != want.Int64() {
			t.Fatalf("MulRhu(%d, %d, %d) = %d, %v; want %s", a, b, d, got, err, want)
		}
		got2, err := Rhu(p.Int64(), d)
		if err != nil || got2 != want.Int64() {
			t.Fatalf("Rhu(%s, %d) = %d, %v; want %s", p, d, got2, err, want)
		}
	}
}

func TestMulRhuOverflow(t *testing.T) {
	tests := []struct {
		name    string
		a, b, d int64
	}{
		{name: "product past int64", a: math.MaxInt64, b: 2, d: 1},
		{name: "128-bit product", a: math.MaxInt64, b: math.MaxInt64, d: math.MaxInt64},
		{name: "negative past int64", a: math.MinInt64, b: 2, d: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := MulRhu(tt.a, tt.b, tt.d); !errors.Is(err, ErrOverflow) {
				t.Fatalf("err = %v, want ErrOverflow", err)
			}
		})
	}
	if _, err := Add(math.MaxInt64, 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Add overflow err = %v", err)
	}
	if _, err := Add(math.MinInt64, -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Add underflow err = %v", err)
	}
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		name    string
		total   Cents
		weights []int64
		want    []Cents
		err     error
	}{
		{name: "even", total: 300, weights: []int64{1, 1, 1}, want: []Cents{100, 100, 100}},
		{name: "tie to lowest index", total: 100, weights: []int64{1, 1, 1}, want: []Cents{34, 33, 33}},
		{name: "largest remainder", total: 10, weights: []int64{1, 2, 4}, want: []Cents{1, 3, 6}},
		{name: "negative total", total: -100, weights: []int64{1, 1, 1}, want: []Cents{-34, -33, -33}},
		{name: "zero weight", total: 5, weights: []int64{0, 1}, want: []Cents{0, 5}},
		{name: "big total", total: math.MaxInt64, weights: []int64{1, 1}, want: []Cents{math.MaxInt64/2 + 1, math.MaxInt64 / 2}},
		{name: "negative weight", total: 5, weights: []int64{-1, 2}, err: ErrBadWeights},
		{name: "all zero", total: 5, weights: []int64{0, 0}, err: ErrBadWeights},
		{name: "none", total: 5, err: ErrBadWeights},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Default.Allocate(tt.total, tt.weights)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Allocate = %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: ARC-I-003
func TestARC_I_003_CentsJSONIntegerOnly(t *testing.T) {
	tests := []struct {
		in   string
		want Cents
		err  error
	}{
		{in: `105050`, want: 105050},
		{in: `-3`, want: -3},
		{in: `12.5`, err: ErrParse},
		{in: `1050.0`, err: ErrParse},
		{in: `"1250"`, err: ErrParse},
		{in: `1e3`, err: ErrParse},
		{in: `null`, err: ErrParse},
		{in: `true`, err: ErrParse},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var v struct {
				AmountCents Cents `json:"amount_cents"`
			}
			err := json.Unmarshal([]byte(`{"amount_cents":`+tt.in+`}`), &v)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && v.AmountCents != tt.want {
				t.Fatalf("got %d, want %d", v.AmountCents, tt.want)
			}
		})
	}
	b, _ := json.Marshal(Cents(1050))
	if string(b) != "1050" {
		t.Fatalf("marshal = %s", b)
	}
}

// rule: MIG-005
func TestMIG_005_ParseDecimalWithoutFloat(t *testing.T) {
	tests := []struct {
		in   string
		want Cents
		err  error
	}{
		{in: "1,234.56", want: 123456},
		{in: "1234.5", want: 123450},
		{in: "0.1", want: 10},
		{in: "1e2", want: 10000},
		{in: "1.5e1", want: 1500},
		{in: "125e-2", want: 125},
		{in: "-12.34", want: -1234},
		{in: "0.10", want: 10},
		{in: "0.100", want: 10},
		{in: "19.999", err: ErrParse},
		{in: "1e-3", err: ErrParse},
		{in: "12,34.00", err: ErrParse},
		{in: "abc", err: ErrParse},
		{in: "", err: ErrParse},
		{in: "1.", err: ErrParse},
		{in: "99999999999999999999", err: ErrOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseDecimal(tt.in)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
	var sum Cents
	for i := 0; i < 10; i++ {
		c, err := ParseJSONNumber(json.Number("0.10"))
		if err != nil {
			t.Fatal(err)
		}
		sum += c
	}
	if sum != 100 {
		t.Fatalf("10 x 0.10 = %d, want 100", sum)
	}
}

// rule: BIL-I-024
func TestBIL_I_024_RatesArePPMNoBasisPoints(t *testing.T) {
	if reflect.TypeOf(PPM(0)).Kind() != reflect.Int64 || reflect.TypeOf(Cents(0)).Kind() != reflect.Int64 {
		t.Fatal("Cents and PPM must be int64")
	}
	var p PPM
	if err := json.Unmarshal([]byte(`1.5`), &p); !errors.Is(err, ErrParse) {
		t.Fatalf("ppm float err = %v", err)
	}
	// No basis-point type anywhere in kernel/ or app/ (CF-01).
	bp := regexp.MustCompile(`(?i)(basis_?points?|^bps?$|bps$|^bp[A-Z_]|[a-z]Bps?$)`)
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	for _, dir := range []string{"kernel", "app"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if ts, ok := n.(*ast.TypeSpec); ok && bp.MatchString(ts.Name.Name) {
					t.Errorf("%s: basis-point type %s; use money.PPM", path, ts.Name.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
