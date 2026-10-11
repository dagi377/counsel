package payment

import "fmt"

// ProviderState is a provider payment object's state as the handler re-fetches it.
type ProviderState string

// Provider states, lowest precedence first. Reversed (dispute lost, chargeback, NSF) is
// terminal.
const (
	ProviderPending           ProviderState = "pending"
	ProviderReceived          ProviderState = "received"
	ProviderPosted            ProviderState = "posted"
	ProviderPartiallyRefunded ProviderState = "partially_refunded"
	ProviderRefunded          ProviderState = "refunded"
	ProviderReversed          ProviderState = "reversed"
)

// precedence ranks provider states; reversed outranks every other state.
var precedence = map[ProviderState]int{
	ProviderPending:           1,
	ProviderReceived:          2,
	ProviderPosted:            3,
	ProviderPartiallyRefunded: 4,
	ProviderRefunded:          5,
	ProviderReversed:          6,
}

// Step is what a provider event does to the payment for its provider object.
type Step struct {
	Create  bool          // create the payment (no payment exists for the object yet)
	Reverse bool          // reverse the payment once (ReverseRows)
	Set     bool          // move the payment to State
	State   ProviderState // the payment's state after the event
}

// NoOp reports whether the event changes nothing.
func (s Step) NoOp() bool { return !s.Create && !s.Reverse && !s.Set }

// ApplyEvent decides what a refund, dispute, chargeback or capture event does. current is
// the payment's state, or "" when no payment exists for the provider object yet; fetched
// is the object's state re-fetched from the provider by provider_ref. The (provider,
// event_id) dedup happens before this, in kernel/inbound.
func ApplyEvent(current, fetched ProviderState) (Step, error) {
	pf, ok := precedence[fetched]
	if !ok {
		return Step{}, fmt.Errorf("provider event: state %q: %w", fetched, ErrInvalid)
	}
	if current == "" {
		// rule: BIL-C-068
		// A dispute that arrives before the capture creates the payment and then reverses
		// it once; order of arrival does not matter.
		return Step{Create: true, Set: true, State: fetched, Reverse: fetched == ProviderReversed}, nil
	}
	pc, ok := precedence[current]
	if !ok {
		return Step{}, fmt.Errorf("provider event: payment state %q: %w", current, ErrInvalid)
	}
	// rule: ARC-I-048
	// State moves only up pending < received < posted < partially_refunded < refunded;
	// reversed is terminal, and a lower-precedence event after a higher one is a no-op.
	if current == ProviderReversed || pf <= pc {
		return Step{State: current}, nil
	}
	return Step{Set: true, State: fetched, Reverse: fetched == ProviderReversed}, nil
}
