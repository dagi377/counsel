package boot

import (
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/settings"
	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// The money kinds get their dedicated effect, never ApprovalEffect; billing.payment_allocation
// only with the gate ports. trust.transfer is never in the money table: the trust services
// register its posting effect, one effect per kind.
func TestMoneyEffects(t *testing.T) {
	always := []string{trust.CashGateKind, bill.KindDelivery, recon.GateSignOff, payment.KindPaymentPlan,
		bill.KindPrebillReview, settings.KindSettingsChange, books.KindOperatingReconciliation, trust.GateRegulatoryRelease}
	ported := []string{payment.KindAllocation}
	set := func(lists ...[]string) map[string]bool {
		m := map[string]bool{}
		for _, l := range lists {
			for _, k := range l {
				m[k] = true
			}
		}
		return m
	}
	kinds := append(append(slices.Clone(always), ported...), withdraw.TransferGateKind)
	tests := []struct {
		name  string
		ports *GatePorts
		want  map[string]bool
	}{
		{"no gate ports", nil, set(always)},
		{"gate ports", testGatePorts(), set(always, ported)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := moneyEffects(testKernel(), tt.ports)
			for _, k := range kinds {
				if _, ok := fx[k]; ok != tt.want[k] {
					t.Errorf("%s wired = %v, want %v", k, ok, tt.want[k])
				}
			}
			for k := range fx {
				for _, a := range ApprovalKinds {
					if a == k {
						t.Errorf("%s is in ApprovalKinds and has a dedicated effect", k)
					}
				}
			}
		})
	}
}

// trust.transfer's one effect is the trust services' posting effect.
func TestTransferEffectFromTrustServices(t *testing.T) {
	if fx := transferEffects(nil); len(fx) != 0 {
		t.Fatalf("no transfer effect built: table = %v, want empty", fx)
	}
	fx := transferEffects(&withdraw.TransferEffect{})
	if _, ok := fx[withdraw.TransferGateKind]; !ok || len(fx) != 1 {
		t.Fatalf("transfer effects = %v, want only %s", fx, withdraw.TransferGateKind)
	}
}
