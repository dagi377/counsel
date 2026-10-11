package boot

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// The money kinds get their dedicated effect, never ApprovalEffect; trust.transfer only with
// the gate ports.
func TestMoneyEffects(t *testing.T) {
	kinds := []string{trust.CashGateKind, bill.KindDelivery, recon.GateSignOff, withdraw.TransferGateKind}
	tests := []struct {
		name  string
		ports *GatePorts
		want  map[string]bool
	}{
		{"no gate ports", nil, map[string]bool{trust.CashGateKind: true, bill.KindDelivery: true, recon.GateSignOff: true}},
		{"gate ports", testGatePorts(), map[string]bool{trust.CashGateKind: true, bill.KindDelivery: true, recon.GateSignOff: true, withdraw.TransferGateKind: true}},
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
