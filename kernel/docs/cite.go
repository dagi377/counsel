package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// QuotePrefix marks the hash algorithm in Citation.QuoteHash.
const QuotePrefix = "sha256:"

// NormalizeQuote is the canonical form of quoted text: NFC, runs of whitespace collapsed
// to one space, trimmed.
//
// rule: ARC-I-037
func NormalizeQuote(s string) string {
	return strings.Join(strings.Fields(norm.NFC.String(s)), " ")
}

// QuoteHash returns "sha256:" + hex(SHA-256(NormalizeQuote(quote))).
//
// rule: ARC-I-037
func QuoteHash(quote string) string {
	sum := sha256.Sum256([]byte(NormalizeQuote(quote)))
	return QuotePrefix + hex.EncodeToString(sum[:])
}

func joinLines(ls []Line) string {
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = l.Text
	}
	return strings.Join(parts, "\n")
}

// Cite returns a citation for lines l0..l1 (inclusive) of a page of this version.
func (t Text) Cite(page, l0, l1 int) (Citation, error) {
	ls, err := t.span(page, l0, l1)
	if err != nil {
		return Citation{}, fmt.Errorf("cite: %w", err)
	}
	// rule: ARC-I-037
	return Citation{VersionID: t.Version, Page: page, FromLine: l0, ToLine: l1, QuoteHash: QuoteHash(joinLines(ls))}, nil
}

// Verify re-hashes the cited lines against this version's stored text at render time. A
// citation to another version, to lines that no longer exist, or to text that changed
// fails. The error never carries the text itself.
func (t Text) Verify(c Citation) error {
	if c.VersionID != t.Version {
		return fmt.Errorf("verify %s: cited version %s: %w", t.Version, c.VersionID, ErrCitationMismatch)
	}
	ls, err := t.span(c.Page, c.FromLine, c.ToLine)
	if err != nil {
		// rule: ARC-I-037
		return fmt.Errorf("verify %s: %w: %w", c.VersionID, ErrCitationMismatch, err)
	}
	// rule: ARC-I-037
	if QuoteHash(joinLines(ls)) != c.QuoteHash {
		return fmt.Errorf("verify %s: %w", c.VersionID, ErrCitationMismatch)
	}
	return nil
}
