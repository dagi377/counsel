package deadlines_test

import (
	"testing"

	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/deadlines"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/kernel/deadline"
)

// The event types the deadline features bind to are the ones the owning modules publish
// (Q-INTEG-13b, DL-EVENTS).
func TestTriggerEventsArePublished(t *testing.T) {
	tests := []struct {
		name, bound, published string
	}{
		{"rule pack published (cal)", deadlines.EventRulePackPublished, deadline.EventPackPublished},
		{"holiday added (rulepacks)", deadlines.EventHolidayAdded, ontario.EventHolidayAdded},
		{"filing receipt (comms)", deadlines.EventFilingReceiptReceived, comms.EventFilingReceiptReceived},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.bound != tt.published {
				t.Fatalf("bound %q, published %q", tt.bound, tt.published)
			}
		})
	}
}
