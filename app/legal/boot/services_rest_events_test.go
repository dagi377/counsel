package boot

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// TestBreakGlassPublishesEnded: the break-glass service boot builds publishes
// break_glass.ended on the kernel emitter once for each grant it expires (EVENTS-8).
func TestBreakGlassPublishesEnded(t *testing.T) {
	tests := []struct {
		name    string
		advance time.Duration
		want    int
	}{
		{"grant still in force", time.Hour, 0},
		{"grant ended", 3 * time.Hour, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), time.UTC)
			em := outboxfake.New()
			bg := withEndedEvents(identity.NewBreakGlass(clk, auditfake.New("grant_id", "starts_at", "ends_at", "user_id")), em)
			g, err := bg.Open(ctx, nil, identity.BreakGlassRequest{
				By:      identity.Principal{Kind: registry.PrincipalUser, UserID: "u-1"},
				Subject: identity.Subject{Kind: "matter", ID: "m-1"}, Reason: "urgent", Explanation: "on call",
				Duration: 2 * time.Hour,
			})
			if err != nil {
				t.Fatal(err)
			}
			clk.Advance(tt.advance)
			if _, err := bg.Expire(ctx, nil); err != nil {
				t.Fatal(err)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("published %d events, want %d", len(msgs), tt.want)
			}
			if tt.want == 1 && (msgs[0].Type != identity.EventBreakGlassEnded || msgs[0].Subject.ID != g.ID) {
				t.Fatalf("message = %+v", msgs[0])
			}
		})
	}
}
