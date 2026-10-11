package intake_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// statusStore is an in-memory intake.LeadStatuses.
type statusStore map[string]intake.LeadStatus

func (s statusStore) SetStatus(_ context.Context, id string, to intake.LeadStatus) (intake.LeadStatus, error) {
	from, ok := s[id]
	switch {
	case !ok:
		return "", intake.ErrNotFound
	case from != intake.LeadOpen && from != intake.LeadAwaitingInfo:
		return from, intake.ErrLeadClosed
	}
	s[id] = to
	return from, nil
}

// rule: ARC-I-030
// rule: ARC-I-051
// POST /leads/{lead_id}/awaiting-info sets the lead awaiting info and publishes one
// intake.lead_awaiting_info; without intake.leads it is 403 and publishes nothing.
func TestStatusHTTPAwaitingInfo(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		lead       string
		start      intake.LeadStatus
		wantStatus int
		wantEvents int
	}{
		{"open lead", eventroutetest.Lawyer, "lead-1", intake.LeadOpen, http.StatusOK, 1},
		{"already awaiting info", eventroutetest.Lawyer, "lead-1", intake.LeadAwaitingInfo, http.StatusOK, 0},
		{"no capability", eventroutetest.NoFirmCap, "lead-1", intake.LeadOpen, http.StatusForbidden, 0},
		{"unknown lead", eventroutetest.Lawyer, "lead-x", intake.LeadOpen, http.StatusNotFound, 0},
		{"converted lead", eventroutetest.Lawyer, "lead-1", intake.LeadConverted, http.StatusConflict, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			leads := &intake.Leads{Statuses: statusStore{"lead-1": tt.start}, Events: em}
			h := &intake.StatusHTTP{Route: eventroutetest.Route(eventroutetest.User(tt.user)), Leads: leads}
			rec := eventroutetest.Post(h.Routes, "/leads/"+tt.lead+"/awaiting-info", "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			msgs := em.Messages()
			if len(msgs) != tt.wantEvents {
				t.Fatalf("events = %d, want %d", len(msgs), tt.wantEvents)
			}
			for _, m := range msgs {
				if m.Type != intake.EventLeadAwaitingInfo || m.Subject.Kind != intake.SubjectLead || m.Subject.ID != tt.lead {
					t.Fatalf("event = %s %+v", m.Type, m.Subject)
				}
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			var out struct {
				LeadID string `json:"lead_id"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.LeadID != tt.lead || out.Status != string(intake.LeadAwaitingInfo) {
				t.Fatalf("body = %s", rec.Body)
			}
		})
	}
}

// An unwired route answers 503 rather than moving anything.
func TestStatusHTTPUnwired(t *testing.T) {
	tests := []struct {
		name string
		h    *intake.StatusHTTP
	}{
		{"no leads", &intake.StatusHTTP{Route: eventroutetest.Route(eventroutetest.User(eventroutetest.Lawyer))}},
		{"no route deps", &intake.StatusHTTP{Route: eventroute.Route{}, Leads: intake.NewLeads(nil, nil, nil, time.Now)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := eventroutetest.Post(tt.h.Routes, "/leads/lead-1/awaiting-info", ""); rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
		})
	}
}
