// Package split divides a bill among the payers of a split matter and groups a combined
// client bill by matter.
package split

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
)

// FlagJointRetainerVerified gates per-payer billing of a joint retainer (BIL-L-029). It
// mirrors legal.FlagJointRetainerVerified, which this package cannot import. Off by default.
const FlagJointRetainerVerified = "billing.joint_retainer_verified"

// Sentinel errors. Each maps to the problem+json code in its comment.
var (
	// ErrSplitNot100 means the payer shares do not sum to 1000000 ppm (SPLIT_NOT_100).
	ErrSplitNot100 = errors.New("split: shares do not sum to 100%")
	// ErrBadShare means a payer share is negative.
	ErrBadShare = errors.New("split: negative share")
	// ErrNoPayers means the split has no payers.
	ErrNoPayers = errors.New("split: no payers")
	// ErrNoClientPayer means applies_to is not "all" and the client payer is not a payer.
	ErrNoClientPayer = errors.New("split: client payer is not in the split")
	// ErrNegativeLine means a line amount is negative; corrections are credit notes.
	ErrNegativeLine = errors.New("split: negative line amount")
	// ErrCrossPayerTrust means trust held for one payer was applied to another payer's bill.
	ErrCrossPayerTrust = errors.New("split: trust belongs to another payer")
	// ErrCrossMatterTrust means trust of one matter was applied to another matter's lines.
	ErrCrossMatterTrust = errors.New("split: trust belongs to another matter")
	// ErrInvariant means the computed portions break BIL-I-018; it signals a bug.
	ErrInvariant = errors.New("split: portions do not add up")
	// ErrUnverified means a valid split was refused because FlagJointRetainerVerified is off.
	ErrUnverified = errors.New("split: joint retainer rule not verified")
)

// AppliesTo selects the lines a split covers.
type AppliesTo string

// AppliesTo values.
const (
	All           AppliesTo = "all"
	Fees          AppliesTo = "fees"
	Disbursements AppliesTo = "disbursements"
)

// Kind is the kind of a bill line.
type Kind string

// Kind values.
const (
	Fee          Kind = "fee"
	Disbursement Kind = "disbursement"
)

// Payer is one payer of a split matter. The payer with the lowest SortOrder is the first payer.
type Payer struct {
	ID        string
	SharePPM  money.PPM
	SortOrder int
}

// Split is one matter split version.
type Split struct {
	Payers        []Payer
	AppliesTo     AppliesTo
	ClientPayerID string // receives every line the split does not cover
}

// Line is one bill line.
type Line struct {
	ID        string
	MatterID  string
	Kind      Kind
	Amount    money.Cents
	SortOrder int
}

// Portion is the part of one line one payer owes.
type Portion struct {
	LineID string
	Amount money.Cents
}

// PayerBill is the separate bill one payer receives.
type PayerBill struct {
	PayerID  string
	Portions []Portion
	Total    money.Cents
}

// Validate checks the payer shares. Shares that break BIL-C-090 are always refused; valid
// shares return ErrUnverified while FlagJointRetainerVerified is off.
func Validate(ctx context.Context, payers []Payer) error {
	if len(payers) == 0 {
		return ErrNoPayers
	}
	var sum int64
	for _, p := range payers {
		if p.SharePPM < 0 {
			return fmt.Errorf("payer %s: %w", p.ID, ErrBadShare)
		}
		sum += int64(p.SharePPM)
	}
	// rule: BIL-C-090
	// rule: BIL-L-029 (verify)
	if sum != money.PPMScale {
		return fmt.Errorf("sum %d ppm: %w", sum, ErrSplitNot100)
	}
	// rule: BIL-L-029 (verify)
	if !flags.Enabled(ctx, FlagJointRetainerVerified) {
		return fmt.Errorf("validate split: %w", ErrUnverified)
	}
	return nil
}

// Shares splits line amounts, in the order given, across shares (index 0 is the first
// payer). It returns portions[line][payer].
func Shares(shares []money.PPM, amounts []money.Cents) ([][]money.Cents, error) {
	n := len(shares)
	if n == 0 {
		return nil, ErrNoPayers
	}
	assigned := make([]int64, n)
	var cum int64
	out := make([][]money.Cents, len(amounts))
	for k, a := range amounts {
		if a < 0 {
			return nil, fmt.Errorf("line %d: %w", k, ErrNegativeLine)
		}
		if int64(a) > math.MaxInt64-cum || cum+int64(a) > math.MaxInt64/money.PPMScale {
			return nil, fmt.Errorf("line %d: %w", k, money.ErrOverflow)
		}
		cum += int64(a)
		parts := make([]int64, n)
		var others int64
		// rule: BIL-C-091
		// Non-first payers take floor(C_k * share / 1e6) less what they already have; the
		// rule asks for floor, not rounding, so this is plain integer division of
		// non-negative values.
		for p := 1; p < n; p++ {
			parts[p] = cum*int64(shares[p])/money.PPMScale - assigned[p]
			others += parts[p]
		}
		parts[0] = int64(a) - others
		// rule: BIL-C-091
		// A line too small for the non-first parts: cut them from the last payer backwards.
		// The cut stays out of assigned, so it carries to later lines.
		for p := n - 1; parts[0] < 0 && p > 0; p-- {
			cut := min(-parts[0], parts[p])
			parts[p] -= cut
			parts[0] += cut
		}
		row := make([]money.Cents, n)
		var sum int64
		for p := range parts {
			assigned[p] += parts[p]
			row[p] = money.Cents(parts[p])
			sum += parts[p]
		}
		// rule: BIL-I-018
		if sum != int64(a) || parts[0] < 0 {
			return nil, fmt.Errorf("line %d: %w", k, ErrInvariant)
		}
		out[k] = row
	}
	return out, nil
}

// Apply splits lines into one independent bill per payer, in payer order. It returns
// ErrUnverified while FlagJointRetainerVerified is off (see Validate).
func Apply(ctx context.Context, s Split, lines []Line) ([]PayerBill, error) {
	// rule: BIL-L-029 (verify)
	if err := Validate(ctx, s.Payers); err != nil {
		return nil, err
	}
	payers := append([]Payer(nil), s.Payers...)
	sort.SliceStable(payers, func(i, j int) bool { return payers[i].SortOrder < payers[j].SortOrder })
	ls := append([]Line(nil), lines...)
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].SortOrder < ls[j].SortOrder })

	bills := make([]PayerBill, len(payers))
	index := map[string]int{}
	shares := make([]money.PPM, len(payers))
	for i, p := range payers {
		bills[i].PayerID = p.ID
		index[p.ID] = i
		shares[i] = p.SharePPM
	}
	// rule: BIL-C-090
	// Lines the split does not cover go whole to the client payer.
	client, hasClient := index[s.ClientPayerID]
	var covered []Line
	var amounts []money.Cents
	for _, l := range ls {
		if l.Amount < 0 {
			return nil, fmt.Errorf("line %s: %w", l.ID, ErrNegativeLine)
		}
		if covers(s.AppliesTo, l.Kind) {
			covered = append(covered, l)
			amounts = append(amounts, l.Amount)
			continue
		}
		if !hasClient {
			return nil, ErrNoClientPayer
		}
		bills[client].add(l.ID, l.Amount)
	}
	portions, err := Shares(shares, amounts)
	if err != nil {
		return nil, err
	}
	// rule: BIL-L-029 (verify)
	// rule: BIL-C-092
	// Each payer gets its own bill: own number, tax, allocations, interest and aging.
	for k, l := range covered {
		for p, amt := range portions[k] {
			bills[p].add(l.ID, amt)
		}
	}
	return bills, nil
}

func (b *PayerBill) add(lineID string, amt money.Cents) {
	b.Portions = append(b.Portions, Portion{LineID: lineID, Amount: amt})
	b.Total += amt
}

func covers(a AppliesTo, k Kind) bool {
	switch a {
	case Fees:
		return k == Fee
	case Disbursements:
		return k == Disbursement
	default:
		return true
	}
}

// ApplyTrust checks that trust held for trustPayerID may pay bill b.
func ApplyTrust(b PayerBill, trustPayerID string) error {
	// rule: BIL-C-092
	if b.PayerID != trustPayerID {
		return ErrCrossPayerTrust
	}
	return nil
}

// MatterGroup is the lines of one matter on a combined client bill.
type MatterGroup struct {
	MatterID string
	Lines    []Line
	Subtotal money.Cents
}

// Combined is a combined client bill: lines grouped by matter. Tax is computed once on Total.
type Combined struct {
	Matters []MatterGroup
	Total   money.Cents
}

// Combine groups lines by matter, in the order each matter first appears by SortOrder.
func Combine(lines []Line) Combined {
	ls := append([]Line(nil), lines...)
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].SortOrder < ls[j].SortOrder })
	var c Combined
	at := map[string]int{}
	// rule: BIL-C-111
	for _, l := range ls {
		i, ok := at[l.MatterID]
		if !ok {
			i = len(c.Matters)
			at[l.MatterID] = i
			c.Matters = append(c.Matters, MatterGroup{MatterID: l.MatterID})
		}
		c.Matters[i].Lines = append(c.Matters[i].Lines, l)
		c.Matters[i].Subtotal += l.Amount
		c.Total += l.Amount
	}
	return c
}

// TrustCap returns the most trust of trustMatterID that may pay lines of lineMatterID:
// that matter's subtotal when the matters match, else ErrCrossMatterTrust.
func (c Combined) TrustCap(trustMatterID, lineMatterID string) (money.Cents, error) {
	// rule: BIL-C-111
	if trustMatterID != lineMatterID {
		return 0, ErrCrossMatterTrust
	}
	for _, g := range c.Matters {
		if g.MatterID == lineMatterID {
			return g.Subtotal, nil
		}
	}
	return 0, nil
}
