// CONTRACT: kernel/money. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-003 subtasks (money.go, allocate.go).

package money

import "errors"

// Cents is an amount of money in integer cents. It is never a float.
type Cents int64

// PPM is a rate or percentage in parts per million: 13% is 130000. There is no
// basis-point type.
type PPM int64

// PPMScale is the denominator of a PPM value.
const PPMScale int64 = 1_000_000

// Sentinel errors.
var (
	// ErrOverflow is returned when a product or quotient does not fit in int64.
	// Arithmetic never wraps.
	ErrOverflow = errors.New("money: overflow")
	// ErrDivideByZero is returned by Rhu when the denominator is zero.
	ErrDivideByZero = errors.New("money: divide by zero")
	// ErrBadWeights is returned by Allocate when a weight is negative or all weights are zero.
	ErrBadWeights = errors.New("money: bad allocation weights")
	// ErrParse is returned when text or JSON is not a valid cent amount.
	ErrParse = errors.New("money: cannot parse amount")
)

// Arithmetic is the rounding and splitting surface of the package. The package-level
// functions Rhu and Allocate implement it; moneyfake.Fake is the in-memory stand-in.
type Arithmetic interface {
	// Rhu returns num/den rounded half away from zero (rhu(5,10)=1, rhu(-5,10)=-1,
	// rhu(4,10)=0). It is the only division used for money.
	Rhu(num, den int64) (int64, error)
	// Allocate splits total across weights by largest remainder. The parts always sum
	// to total. Ties in remainder go to the lowest index.
	Allocate(total Cents, weights []int64) ([]Cents, error)
}
