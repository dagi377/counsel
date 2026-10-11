package comms

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
)

type inboundStub struct {
	id    string
	calls int
}

func (s *inboundStub) Receive(context.Context, InboundEmail) (string, error) {
	s.calls++
	return s.id, nil
}

// rule: ARC-I-051
func TestInboundRoute(t *testing.T) {
	const body = `{"connector_id":"k1","external_id":"x1","participants":[],"occurred_at":"2026-10-11T14:00:00Z"}`
	tests := []struct {
		name   string
		user   string
		id     string
		status int
		calls  int
	}{
		{"no capability", eventroutetest.NoFirmCap, "ci1", 403, 0},
		{"stores the email", eventroutetest.Lawyer, "ci1", 201, 1},
		{"redelivery", eventroutetest.Lawyer, "", 200, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := &inboundStub{id: tt.id}
			h := &InboundHTTP{Route: eventroutetest.Route(eventroutetest.User(tt.user)), Inbound: in}
			rec := eventroutetest.Post(h.Routes, "/emails/inbound", body)
			if rec.Code != tt.status || in.calls != tt.calls {
				t.Fatalf("status %d calls %d, want %d and %d: %s", rec.Code, in.calls, tt.status, tt.calls, rec.Body)
			}
		})
	}
}
