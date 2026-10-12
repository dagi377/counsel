package boot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Boot makes the gate engine publish one gate.decided per decision, acting for the decider.
func TestARC_I_030_WireGateDecided(t *testing.T) {
	tests := []struct {
		name  string
		g     gate.Engine
		wired bool
	}{
		{"postgres engine", gate.NewPG(), true},
		{"engine without the hook", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if got := wireGateDecided(tt.g, em); got != tt.wired {
				t.Fatalf("wired = %v, want %v", got, tt.wired)
			}
			if !tt.wired {
				return
			}
			hook := gateDecidedPublisher(em)
			if err := hook(context.Background(), nil, gate.Decided{GateID: "g1", Version: 2, Decision: "reject", DeciderID: "u1", Final: true}); err != nil {
				t.Fatal(err)
			}
			msgs := em.Messages()
			if len(msgs) != 1 || msgs[0].Type != billing.EventGateDecided || msgs[0].Subject.ID != "g1" {
				t.Fatalf("events = %+v", msgs)
			}
			var b billing.GateDecided
			if err := json.Unmarshal(msgs[0].Payload, &b); err != nil {
				t.Fatal(err)
			}
			want := billing.GateDecided{GateID: "g1", Version: 2, Decision: "reject", DeciderID: "u1", Final: true, UserID: "u1"}
			if b != want {
				t.Fatalf("body = %+v, want %+v", b, want)
			}
		})
	}
}
