package clio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

var (
	// ErrOpeningUnverified is returned while the By-Law 9 flag is off.
	ErrOpeningUnverified = errors.New("clio: opening balances: by-law 9 rules not verified")
	// ErrTrustLocked is returned until G-IMP is fully signed: the firm's trust area is read-only.
	ErrTrustLocked = errors.New("clio: trust locked pending G-IMP")
	// ErrBadOpeningLine is returned for a malformed opening line.
	ErrBadOpeningLine = errors.New("clio: bad opening balance line")
)

// SignerRole is a role that must sign the G-IMP opening-balance list.
type SignerRole string

// The three roles G-IMP needs (MIG-033).
const (
	SignerOwner      SignerRole = "owner"
	SignerLicensee   SignerRole = "licensee"
	SignerBookkeeper SignerRole = "bookkeeper"
)

var requiredSigners = []SignerRole{SignerOwner, SignerLicensee, SignerBookkeeper}

// OpeningLine is one client ledger's opening balance in the live book. AmountCents > 0.
type OpeningLine struct {
	LedgerID    string
	AmountCents int64
}

// OpeningSignature is one G-IMP signature over a list hash. A licensee signature counts only
// with signing authority on the account. Agent principals never sign.
type OpeningSignature struct {
	UserID           string
	Role             SignerRole
	ListHash         string
	SigningAuthority bool
	IsAgent          bool
}

// OpeningSignoff is the G-IMP sign-off for one trust account.
type OpeningSignoff struct {
	AccountID       string
	Lines           []OpeningLine
	StatementSHA256 string // hex hash of the bank statement file
	Signatures      []OpeningSignature
}

// OpeningPoster writes one opening_balance row into the live trust book.
type OpeningPoster interface {
	PostOpening(ctx context.Context, accountID string, line OpeningLine) error
}

// OpeningListHash is the hash every signer signs: the per-ledger list, in ledger order, and
// the bank statement file hash. Any change to the list changes the hash.
func OpeningListHash(lines []OpeningLine, statementSHA256 string) string {
	sorted := append([]OpeningLine(nil), lines...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LedgerID < sorted[j].LedgerID })
	h := sha256.New()
	for _, l := range sorted {
		h.Write([]byte(l.LedgerID))
		h.Write([]byte{0})
		h.Write([]byte(strconv.FormatInt(l.AmountCents, 10)))
		h.Write([]byte{'\n'})
	}
	h.Write([]byte(statementSHA256))
	return hex.EncodeToString(h.Sum(nil))
}

// CheckOpeningSigned returns nil only when owner, a licensee with signing authority and the
// bookkeeper have each signed the current list hash. Signatures over another hash are void.
func CheckOpeningSigned(ctx context.Context, s OpeningSignoff) error {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return ErrOpeningUnverified
	}
	if s.AccountID == "" || s.StatementSHA256 == "" || len(s.Lines) == 0 {
		return fmt.Errorf("check opening: %w", ErrBadOpeningLine)
	}
	seen := make(map[string]bool, len(s.Lines))
	for _, l := range s.Lines {
		if l.LedgerID == "" || l.AmountCents <= 0 || seen[l.LedgerID] {
			return fmt.Errorf("check opening: %w", ErrBadOpeningLine)
		}
		seen[l.LedgerID] = true
	}
	// rule: MIG-033
	want := OpeningListHash(s.Lines, s.StatementSHA256)
	signed := make(map[SignerRole]bool, len(requiredSigners))
	for _, sig := range s.Signatures {
		if sig.IsAgent || sig.UserID == "" || sig.ListHash != want {
			continue
		}
		if sig.Role == SignerLicensee && !sig.SigningAuthority {
			continue
		}
		signed[sig.Role] = true
	}
	for _, r := range requiredSigners {
		if !signed[r] {
			return fmt.Errorf("check opening %s unsigned: %w", r, ErrTrustLocked)
		}
	}
	return nil
}

// PostOpeningBalances posts one opening_balance row per line, only after G-IMP sign-off.
func PostOpeningBalances(ctx context.Context, s OpeningSignoff, p OpeningPoster) error {
	// rule: MIG-033
	if err := CheckOpeningSigned(ctx, s); err != nil {
		return fmt.Errorf("post opening balances: %w", err)
	}
	for _, l := range s.Lines {
		if err := p.PostOpening(ctx, s.AccountID, l); err != nil {
			return fmt.Errorf("post opening balance: %w", err)
		}
	}
	return nil
}
