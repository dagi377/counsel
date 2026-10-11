package intake

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/party/partyfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// A portal form submission that creates a lead publishes exactly one
// identity.portal_submission; a repeat with the same idempotency key publishes nothing more.
func TestSubmitPublishesPortalSubmission(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	valid := Submission{FormID: "f-pub", ContactName: "A Person", ContactEmail: "a@example.test", IdempotencyKey: "k1"}
	tests := []struct {
		name  string
		times int
		want  int
	}{
		{name: "one submission", times: 1, want: 1},
		{name: "repeated submission", times: 2, want: 1},
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
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			m := msgs[0]
			if m.Type != EventPortalSubmission || m.Subject.Kind != SubjectLead || m.Subject.ID != l.ID {
				t.Fatalf("event = %s %+v, want lead %s", m.Type, m.Subject, l.ID)
			}
		})
	}
}
