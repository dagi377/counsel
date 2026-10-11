package boot

import (
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

func TestPGFeeBillsSQL(t *testing.T) {
	tests := []struct{ name, want string }{
		// rule: BIL-L-035 (verify)
		{"balance net of reserved fee transfers", "bb.balance_cents - bb.reserved_cents"},
		{"delivery time read", "b.delivered_at"},
		{"matter read", "b.matter_id"},
		{"bill locked", "FOR UPDATE OF b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(feeBillSQL, tt.want) {
				t.Fatalf("feeBillSQL lacks %q", tt.want)
			}
		})
	}
}

func TestFeeBillsDefault(t *testing.T) {
	tests := []struct {
		name string
		in   withdraw.FeeBills
		pg   bool
	}{
		{"nil uses Postgres", nil, true},
		{"set port kept", stubFeeBills{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, isPG := feeBills(tt.in).(PGFeeBills)
			if isPG != tt.pg {
				t.Fatalf("feeBills(%T) is PG = %v", tt.in, isPG)
			}
		})
	}
}

type stubFeeBills struct{ withdraw.FeeBills }
