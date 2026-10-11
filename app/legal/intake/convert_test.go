package intake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/conflict"
)

type memLifecycle struct {
	leads  map[string]Lead
	sealed map[string]string // lead id -> retention class
}

func (m *memLifecycle) Lead(_ context.Context, id string) (Lead, error) {
	l, ok := m.leads[id]
	if !ok {
		return Lead{}, ErrNotFound
	}
	return l, nil
}

func (m *memLifecycle) MarkConverted(_ context.Context, id, matterID string) (Lead, error) {
	l := m.leads[id]
	l.Status, l.MatterID = LeadConverted, matterID
	m.leads[id] = l
	return l, nil
}

func (m *memLifecycle) Seal(_ context.Context, id, class string) (Lead, error) {
	l := m.leads[id]
	l.Status = LeadSealedProspect
	m.leads[id] = l
	m.sealed[id] = class
	return l, nil
}

type outcomeSet map[string]bool

func (o outcomeSet) RequireOutcome(_ context.Context, leadID string) error {
	if !o[leadID] {
		return ErrOutcomeRequired
	}
	return nil
}

type runsBySubject map[string][]conflict.Run

func (r runsBySubject) Runs(_ context.Context, subject string) ([]conflict.Run, error) {
	return r[subject], nil
}

type countingOpener struct{ opened []string }

func (c *countingOpener) OpenFromLead(_ context.Context, l Lead, _ string) (string, error) {
	c.opened = append(c.opened, l.ID)
	return "matter-" + l.ID, nil
}

func TestConvert(t *testing.T) {
	tests := []struct {
		name       string
		lead       Lead
		outcome    bool
		runs       []conflict.Run
		by         string
		wantErr    error
		wantMatter string
	}{
		{
			name: "outcome and G2 cleared converts", outcome: true, by: "u-lawyer",
			lead:       Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-1"},
			runs:       []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}},
			wantMatter: "matter-L1",
		},
		{
			name: "no consultation outcome", outcome: false, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-1"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}},
			wantErr: ErrOutcomeRequired,
		},
		{
			name: "no conflict check opened", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadOpen},
			wantErr: ErrConflictNotCleared,
		},
		{
			name: "G2 still open", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-1"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusOpen}},
			wantErr: ErrConflictNotCleared,
		},
		{
			name: "G2 rejected", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-1"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusRejected}},
			wantErr: ErrConflictNotCleared,
		},
		{
			name: "referenced run is not the cleared one", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-2"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}, {ID: "run-2", Status: conflict.StatusOpen}},
			wantErr: ErrConflictNotCleared,
		},
		{
			name: "referenced run missing", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-9"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}},
			wantErr: ErrConflictNotCleared,
		},
		{
			name: "already converted", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadConverted, ConflictRef: "run-1", MatterID: "m-0"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}},
			wantErr: ErrLeadClosed,
		},
		{
			name: "sealed prospect", outcome: true, by: "u-lawyer",
			lead:    Lead{ID: "L1", Status: LeadSealedProspect, ConflictRef: "run-1"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}},
			wantErr: ErrLeadClosed,
		},
		{
			name: "missing actor", outcome: true, by: "",
			lead:    Lead{ID: "L1", Status: LeadOpen, ConflictRef: "run-1"},
			runs:    []conflict.Run{{ID: "run-1", Status: conflict.StatusCleared}},
			wantErr: ErrInvalid,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memLifecycle{leads: map[string]Lead{tc.lead.ID: tc.lead}, sealed: map[string]string{}}
			opener := &countingOpener{}
			svc := NewConversions(store, outcomeSet{tc.lead.ID: tc.outcome}, runsBySubject{tc.lead.ID: tc.runs}, opener)
			got, err := svc.Convert(context.Background(), tc.lead.ID, tc.by)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Convert err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(opener.opened) != 0 {
					t.Fatalf("opened a matter on refusal: %v", opener.opened)
				}
				if store.leads[tc.lead.ID] != tc.lead {
					t.Fatalf("lead changed on refusal: %+v", store.leads[tc.lead.ID])
				}
				return
			}
			l := store.leads[tc.lead.ID]
			if got != tc.wantMatter || l.Status != LeadConverted || l.MatterID != tc.wantMatter {
				t.Fatalf("Convert = %q, lead = %+v; want matter %q", got, l, tc.wantMatter)
			}
		})
	}
}

func TestDecline(t *testing.T) {
	tests := []struct {
		name    string
		lead    Lead
		by      string
		wantErr error
	}{
		{"open lead sealed under RC-LEAD", Lead{ID: "L1", Status: LeadOpen}, "u-lawyer", nil},
		{"converted lead", Lead{ID: "L1", Status: LeadConverted, MatterID: "m-1"}, "u-lawyer", ErrLeadClosed},
		{"already sealed", Lead{ID: "L1", Status: LeadSealedProspect}, "u-lawyer", ErrLeadClosed},
		{"missing actor", Lead{ID: "L1", Status: LeadOpen}, " ", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memLifecycle{leads: map[string]Lead{tc.lead.ID: tc.lead}, sealed: map[string]string{}}
			svc := NewConversions(store, outcomeSet{}, runsBySubject{}, &countingOpener{})
			got, err := svc.Decline(context.Background(), tc.lead.ID, tc.by)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Decline err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if _, sealed := store.sealed[tc.lead.ID]; sealed {
					t.Fatal("sealed on refusal")
				}
				return
			}
			if got.Status != LeadSealedProspect || store.sealed[tc.lead.ID] != "RC-LEAD" {
				t.Fatalf("Decline = %+v, class %q; want sealed_prospect under RC-LEAD", got, store.sealed[tc.lead.ID])
			}
		})
	}
}

func TestDeclineUnknownLead(t *testing.T) {
	store := &memLifecycle{leads: map[string]Lead{}, sealed: map[string]string{}}
	svc := NewConversions(store, outcomeSet{}, runsBySubject{}, &countingOpener{})
	if _, err := svc.Decline(context.Background(), "nope", "u"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Decline err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Convert(context.Background(), "nope", "u"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Convert err = %v, want ErrNotFound", err)
	}
}
