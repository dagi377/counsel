package audit

import (
	"errors"
	"fmt"
	"testing"
)

// appendRows builds a chain of n rows as the audit.chain trigger would.
func appendRows(n int) []ChainRow {
	prev := Genesis
	rows := make([]ChainRow, 0, n)
	for i := 1; i <= n; i++ {
		c := []byte(fmt.Sprintf(`{"seq":%d,"action":"gate.decide","detail":{"n":"%d"}}`, i, i))
		r := ChainRow{Seq: Seq(i), PrevHash: prev, Canonical: c, Hash: HashRow(prev, c)}
		rows = append(rows, r)
		prev = r.Hash
	}
	return rows
}

func TestARC_I_043_HashChain(t *testing.T) {
	rows := appendRows(3)
	for i, r := range rows {
		prev := Genesis
		if i > 0 {
			prev = rows[i-1].Hash
		}
		if want := HashRow(prev, r.Canonical); string(want) != string(r.Hash) {
			t.Fatalf("hash[%d] != sha256(prev_hash || canonical(row))", i+1)
		}
	}
	tests := []struct {
		name    string
		tamper  func(rows []ChainRow)
		wantErr error
		wantSeq string
	}{
		{"intact", func([]ChainRow) {}, nil, ""},
		{"flipped byte in row 2", func(r []ChainRow) { r[1].Canonical[5] ^= 0x01 }, ErrChainBroken, "seq 2"},
		{"flipped stored hash in row 2", func(r []ChainRow) { r[1].Hash[0] ^= 0x01 }, ErrChainBroken, "seq 2"},
		{"relinked row 3", func(r []ChainRow) { r[2].PrevHash = Genesis }, ErrChainBroken, "seq 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := appendRows(3)
			tt.tamper(rows)
			err := VerifyChain(Genesis, rows)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyChain err = %v, want %v", err, tt.wantErr)
			}
			if err != nil && !contains(err.Error(), tt.wantSeq) {
				t.Fatalf("VerifyChain err = %v, want first break at %s", err, tt.wantSeq)
			}
		})
	}
}

func TestMIG_066_NoRehashOfOldEvents(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(rows []ChainRow)
		wantErr error
	}{
		{"untouched chain verifies", func([]ChainRow) {}, nil},
		{"reordered keys in old event", func(r []ChainRow) {
			r[0].Canonical = []byte(`{"action":"gate.decide","seq":1,"detail":{"n":"1"}}`)
		}, ErrChainBroken},
		{"old event rehashed in place", func(r []ChainRow) {
			r[0].Canonical = []byte(`{"seq":1,"v":2}`)
			r[0].Hash = HashRow(r[0].PrevHash, r[0].Canonical)
		}, ErrChainBroken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := appendRows(10)
			tt.mutate(rows)
			if err := VerifyChain(Genesis, rows); !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyChain err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestBIL_I_003_AppendOnly(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(rows []ChainRow) []ChainRow
		prev    func(rows []ChainRow) []byte
		wantErr error
	}{
		{"deleted middle row", func(r []ChainRow) []ChainRow { return append(r[:1:1], r[2:]...) }, nil, ErrChainBroken},
		{"deleted first row", func(r []ChainRow) []ChainRow { return r[1:] }, nil, ErrChainBroken},
		{"suffix with anchor verifies", func(r []ChainRow) []ChainRow { return r[1:] }, func(r []ChainRow) []byte { return r[0].Hash }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := appendRows(4)
			prev := Genesis
			if tt.prev != nil {
				prev = tt.prev(rows)
			}
			if err := VerifyChain(prev, tt.edit(rows)); !errors.Is(err, tt.wantErr) {
				t.Fatalf("VerifyChain err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
