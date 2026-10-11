package audit

import (
	"bytes"
	"crypto/sha256"
	"fmt"
)

// Genesis is the prev_hash of the first event in a chain.
var Genesis = []byte{0x00}

// ChainRow is one stored link: the row's canonical bytes and the hashes stored with it.
// Canonical is recomputed from the row's columns at verify time, never read from storage.
type ChainRow struct {
	Seq       Seq
	PrevHash  []byte
	Hash      []byte
	Canonical []byte
}

// rule: ARC-I-043
// HashRow returns sha256(prev || canonical), the hash stored with a row.
func HashRow(prev, canonical []byte) []byte {
	h := sha256.New()
	h.Write(prev)
	h.Write(canonical)
	return h.Sum(nil)
}

// VerifyChain checks rows in order. prev is the stored hash of the event before rows[0]
// (Genesis when rows[0] is the first event). It returns ErrChainBroken naming the first
// row whose sequence, link or hash does not match.
func VerifyChain(prev []byte, rows []ChainRow) error {
	for i, r := range rows {
		// rule: BIL-I-003
		// The chain is append-only: a removed row shows as a sequence gap.
		if i > 0 && r.Seq != rows[i-1].Seq+1 {
			return fmt.Errorf("verify seq %d: gap after %d: %w", r.Seq, rows[i-1].Seq, ErrChainBroken)
		}
		if !bytes.Equal(r.PrevHash, prev) {
			return fmt.Errorf("verify seq %d link: %w", r.Seq, ErrChainBroken)
		}
		// rule: MIG-066
		// Old rows are never re-hashed: the stored hash must match the row as it is now.
		if !bytes.Equal(HashRow(r.PrevHash, r.Canonical), r.Hash) {
			return fmt.Errorf("verify seq %d hash: %w", r.Seq, ErrChainBroken)
		}
		prev = r.Hash
	}
	return nil
}
