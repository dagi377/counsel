package deadlines

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
)

type calendarStub struct{ calls int }

func (s *calendarStub) Add(context.Context, NewCalendarEvent) (string, error) {
	s.calls++
	return "e1", nil
}

// rule: ARC-I-042
// rule: ARC-I-051
func TestCalendarRouteAccess(t *testing.T) {
	const onMatter = `{"calendar_id":"c1","title":"Meeting","type":"meeting","starts_at":"2026-10-20T14:00:00Z","ends_at":"2026-10-20T15:00:00Z","matter_id":"m1"}`
	const noMatter = `{"calendar_id":"c1","title":"Meeting","type":"meeting","starts_at":"2026-10-20T14:00:00Z","ends_at":"2026-10-20T15:00:00Z"}`
	tests := []struct {
		name   string
		user   string
		body   string
		status int
		calls  int
	}{
		{"walled matter is not found", eventroutetest.Walled, onMatter, 404, 0},
		{"no capability on the matter", eventroutetest.NoCap, onMatter, 403, 0},
		{"allowed user", eventroutetest.Lawyer, onMatter, 201, 1},
		{"no matter checks firm-wide only", eventroutetest.NoCap, noMatter, 201, 1},
		{"no capability firm-wide", eventroutetest.NoFirmCap, noMatter, 403, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cal := &calendarStub{}
			h := &CalendarHTTP{Route: eventroutetest.Route(eventroutetest.User(tt.user)), Calendar: cal}
			rec := eventroutetest.Post(h.Routes, "/calendar-events", tt.body)
			if rec.Code != tt.status || cal.calls != tt.calls {
				t.Fatalf("status %d calls %d, want %d and %d: %s", rec.Code, cal.calls, tt.status, tt.calls, rec.Body)
			}
		})
	}
}
