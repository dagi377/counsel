package party

import (
	"context"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Creating an organization client publishes exactly one party.organization_client_created
// in the insert tx; any other contact publishes nothing.
func TestCreatePublishesOrganizationClientCreated(t *testing.T) {
	okRow := stubRow{vals: []any{"c1", "proposed", created, 1}}
	tests := []struct {
		name   string
		kind   Kind
		client bool
		want   int
	}{
		{name: "organization client", kind: Organization, client: true, want: 1},
		{name: "organization not a client", kind: Organization, client: false},
		{name: "person client", kind: Person, client: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			s := NewStore(&stubDB{tx: &stubTx{row: okRow}})
			s.Events = em
			in := NewContact{Kind: tt.kind, LegalName: "L", DisplayName: "D", IsClient: tt.client, CreatedBy: "u1"}
			if _, err := s.Create(context.Background(), in); err != nil {
				t.Fatalf("Create: %v", err)
			}
			msgs := em.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("events = %d, want %d", len(msgs), tt.want)
			}
			if tt.want == 0 {
				return
			}
			m := msgs[0]
			if m.Type != EventOrganizationClientCreated || m.Subject.Kind != SubjectContact || m.Subject.ID != "c1" {
				t.Fatalf("event = %s %+v", m.Type, m.Subject)
			}
		})
	}
}
