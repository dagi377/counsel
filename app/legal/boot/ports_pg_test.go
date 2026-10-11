package boot

import (
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/matter/matterfake"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

func TestWithPostgresFillsNilPorts(t *testing.T) {
	tp := TrustPorts{}.WithPostgres(dbfake.New())
	cp := CreditPorts{}.WithPostgres(ledgerfake.New(ledger.Date{}), matterfake.New(time.Now, nil))
	tests := []struct {
		name string
		ok   bool
	}{
		{"trust Users is PGDirectory", is[*signatory.PGDirectory](tp.Users)},
		{"trust Signatories is PGService", is[*signatory.PGService](tp.Signatories)},
		{"trust Signers is PGSigners", is[withdraw.PGSigners](tp.Signers)},
		{"trust Firm is PGFirm", is[*withdraw.PGFirm](tp.Firm)},
		{"credit Store is PGWriteOffs", is[credit.PGWriteOffs](cp.Store)},
		{"credit Bills is PGWriteOffs", is[credit.PGWriteOffs](cp.Bills)},
		// rule: PORT-16
		{"credit Ledger is GLBadDebt", is[credit.GLBadDebt](cp.Ledger)},
		// rule: PORT-18
		{"write-offs read lawyers through MatterLawyers", cp.Store.(credit.PGWriteOffs).Lawyers.(MatterLawyers).Matters != nil},
		// rule: PORT-11
		{"PG signatory effects run on the gate tx", signatoryEffects(tp.Signatories)[signatory.KindSigningAuthority] != nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("not wired")
			}
		})
	}
}

func is[T any](v any) bool { _, ok := v.(T); return ok }
