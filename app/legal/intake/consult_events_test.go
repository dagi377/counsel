package intake

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Booking a consultation publishes exactly one consultation.booked with ids and the start
// time; a refused booking publishes nothing.
func TestBookPublishesConsultationBooked(t *testing.T) {
	at := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		lead    Lead
		with    string
		want    int
		wantErr error
	}{
		{"open lead", Lead{ID: "lead-1", Status: LeadOpen}, "u-lawyer", 1, nil},
		{"no lawyer", Lead{ID: "lead-1", Status: LeadOpen}, "", 0, ErrInvalid},
		{"converted lead", Lead{ID: "lead-1", Status: LeadConverted}, "u-lawyer", 0, ErrLeadClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			s := NewConsultations(memLeadReader{tt.lead.ID: tt.lead}, newMemConsults())
			s.Events = em
			c, err := s.Book(context.Background(), "lead-1", tt.with, at)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Book err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			if tt.want == 0 {
				return
			}
			m := msgs[0]
			if m.Type != EventConsultationBooked || m.Subject.Kind != SubjectConsultation || m.Subject.ID != c.ID {
				t.Fatalf("event = %s %+v", m.Type, m.Subject)
			}
			var body map[string]any
			if err := json.Unmarshal(m.Payload, &body); err != nil {
				t.Fatal(err)
			}
			if body["lead_id"] != "lead-1" || body["user_id"] != "u-lawyer" || len(body) != 4 {
				t.Fatalf("payload = %s", m.Payload)
			}
		})
	}
}
