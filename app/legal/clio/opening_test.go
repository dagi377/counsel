package clio

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/flags"
)

type recordPoster struct{ posted []OpeningLine }

func (r *recordPoster) PostOpening(_ context.Context, _ string, l OpeningLine) error {
	r.posted = append(r.posted, l)
	return nil
}

func flagCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBylaw9Verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagBylaw9Verified, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// rule: MIG-033
func TestMIG_033_OpeningPostedOnlyAfterGIMP(t *testing.T) {
	lines := []OpeningLine{{LedgerID: "L-2", AmountCents: 500}, {LedgerID: "L-1", AmountCents: 3_000_000}}
	const stmt = "abc123"
	h := OpeningListHash(lines, stmt)
	changed := OpeningListHash([]OpeningLine{{LedgerID: "L-1", AmountCents: 3_000_001}, {LedgerID: "L-2", AmountCents: 500}}, stmt)
	owner := OpeningSignature{UserID: "u-own", Role: SignerOwner, ListHash: h}
	lic := OpeningSignature{UserID: "u-lic", Role: SignerLicensee, ListHash: h, SigningAuthority: true}
	book := OpeningSignature{UserID: "u-book", Role: SignerBookkeeper, ListHash: h}

	tests := []struct {
		name   string
		flagOn bool
		lines  []OpeningLine
		sigs   []OpeningSignature
		want   error
	}{
		{"flag off", false, lines, []OpeningSignature{owner, lic, book}, ErrOpeningUnverified},
		{"all three signed", true, lines, []OpeningSignature{owner, lic, book}, nil},
		{"licensee missing", true, lines, []OpeningSignature{owner, book}, ErrTrustLocked},
		{"licensee without signing authority", true, lines,
			[]OpeningSignature{owner, {UserID: "u-lic", Role: SignerLicensee, ListHash: h}, book}, ErrTrustLocked},
		{"agent signature ignored", true, lines,
			[]OpeningSignature{owner, lic, {UserID: "agent", Role: SignerBookkeeper, ListHash: h, IsAgent: true}}, ErrTrustLocked},
		{"list changed after signing voids signatures", true, lines,
			[]OpeningSignature{owner, lic, {UserID: "u-book", Role: SignerBookkeeper, ListHash: changed}}, ErrTrustLocked},
		{"zero amount line", true, []OpeningLine{{LedgerID: "L-1"}}, nil, ErrBadOpeningLine},
		{"duplicate ledger", true, []OpeningLine{{LedgerID: "L-1", AmountCents: 1}, {LedgerID: "L-1", AmountCents: 1}}, nil, ErrBadOpeningLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &recordPoster{}
			s := OpeningSignoff{AccountID: "A-1", Lines: tt.lines, StatementSHA256: stmt, Signatures: tt.sigs}
			err := PostOpeningBalances(flagCtx(t, tt.flagOn), s, p)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want != nil && len(p.posted) != 0 {
				t.Fatalf("posted %d lines before sign-off", len(p.posted))
			}
			if tt.want == nil && len(p.posted) != len(tt.lines) {
				t.Fatalf("posted %d lines, want %d", len(p.posted), len(tt.lines))
			}
		})
	}
}

func TestOpeningListHashOrderIndependent(t *testing.T) {
	a := []OpeningLine{{LedgerID: "L-1", AmountCents: 1}, {LedgerID: "L-2", AmountCents: 2}}
	b := []OpeningLine{a[1], a[0]}
	tests := []struct {
		name string
		x, y string
		same bool
	}{
		{"order", OpeningListHash(a, "s"), OpeningListHash(b, "s"), true},
		{"statement hash", OpeningListHash(a, "s"), OpeningListHash(a, "t"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if (tt.x == tt.y) != tt.same {
				t.Fatalf("hash equal = %v, want %v", tt.x == tt.y, tt.same)
			}
		})
	}
}
