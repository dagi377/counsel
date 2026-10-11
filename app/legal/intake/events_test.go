package intake

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/party/partyfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// A portal form submission that creates a lead publishes exactly one lead.created and one
// identity.portal_submission; a repeat with the same idempotency key publishes nothing more.
func TestSubmitPublishesLeadEvents(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	valid := Submission{FormID: "f-pub", ContactName: "A Person", ContactEmail: "a@example.test", IdempotencyKey: "k1"}
	tests := []struct {
		name  string
		times int
	}{
		{name: "one submission", times: 1},
		{name: "repeated submission", times: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			forms := memForms{"f-pub": {ID: "f-pub", Published: true}}
			svc := NewLeads(forms, &memLeads{byKey: map[string]Lead{}}, partyfake.New(func() time.Time { return now }), func() time.Time { return now })
			svc.Events = em
			var l Lead
			for i := 0; i < tt.times; i++ {
				var err error
				if l, err = svc.Submit(context.Background(), valid); err != nil {
					t.Fatalf("Submit: %v", err)
				}
			}
			count := map[string]int{}
			for _, m := range em.Messages() {
				if m.Subject.Kind != SubjectLead || m.Subject.ID != l.ID {
					t.Fatalf("event %s subject %+v, want lead %s", m.Type, m.Subject, l.ID)
				}
				count[m.Type]++
			}
			for _, typ := range []string{EventLeadCreated, EventPortalSubmission} {
				if count[typ] != 1 {
					t.Fatalf("%s events = %d, want 1 (all %v)", typ, count[typ], count)
				}
			}
			if len(count) != 2 {
				t.Fatalf("events = %v, want only %s and %s", count, EventLeadCreated, EventPortalSubmission)
			}
		})
	}
}

// memStatuses is an in-memory LeadStatuses.
type memStatuses struct {
	mu sync.Mutex
	m  map[string]LeadStatus
}

func (s *memStatuses) SetStatus(_ context.Context, id string, to LeadStatus) (LeadStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from, ok := s.m[id]
	switch {
	case !ok:
		return "", ErrNotFound
	case from == LeadConverted || from == LeadDeclined || from == LeadSealedProspect:
		return "", ErrLeadClosed
	}
	s.m[id] = to
	return from, nil
}

// rule: ARC-I-030
// Moving a lead into awaiting info publishes exactly one intake.lead_awaiting_info; moving it
// again, or to open, publishes nothing; a closed or unknown lead fails and publishes nothing.
func TestSetStatusPublishesAwaitingInfo(t *testing.T) {
	tests := []struct {
		name    string
		start   LeadStatus
		moves   []LeadStatus
		want    int
		wantErr error
	}{
		{"open to awaiting info", LeadOpen, []LeadStatus{LeadAwaitingInfo}, 1, nil},
		{"awaiting info twice", LeadOpen, []LeadStatus{LeadAwaitingInfo, LeadAwaitingInfo}, 1, nil},
		{"back to open", LeadAwaitingInfo, []LeadStatus{LeadOpen}, 0, nil},
		{"round trip", LeadOpen, []LeadStatus{LeadAwaitingInfo, LeadOpen, LeadAwaitingInfo}, 2, nil},
		{"converted lead", LeadConverted, []LeadStatus{LeadAwaitingInfo}, 0, ErrLeadClosed},
		{"unknown lead", "", []LeadStatus{LeadAwaitingInfo}, 0, ErrNotFound},
		{"status not settable", LeadOpen, []LeadStatus{LeadConverted}, 0, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			st := &memStatuses{m: map[string]LeadStatus{}}
			if tt.start != "" {
				st.m["lead-1"] = tt.start
			}
			svc := NewLeads(memForms{}, &memLeads{byKey: map[string]Lead{}}, nil, time.Now)
			svc.Events, svc.Statuses = em, st
			var err error
			for _, to := range tt.moves {
				if err = svc.SetStatus(context.Background(), "lead-1", to); err != nil {
					break
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetStatus err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			for _, m := range msgs {
				if m.Type != EventLeadAwaitingInfo || m.Subject.Kind != SubjectLead || m.Subject.ID != "lead-1" {
					t.Fatalf("event = %s %+v", m.Type, m.Subject)
				}
			}
		})
	}
}
