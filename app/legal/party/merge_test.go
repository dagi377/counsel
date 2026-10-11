package party_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/party/partyfake"
)

func fixedNow() time.Time { return time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) }

// mergedStore returns a contact in the merged state, which the fake cannot produce.
type mergedStore struct{ party.Contacts }

func (mergedStore) Get(_ context.Context, id party.ContactID) (party.Contact, error) {
	return party.Contact{ID: id, State: party.Merged}, nil
}

// rule: ARC-I-040
func TestARC_I_040_IntakeContactIsPartyContact(t *testing.T) {
	tests := []struct {
		name    string
		sub     intake.Submission
		wantErr error
	}{
		{"person from form", intake.Submission{FormID: "f1", ContactName: "  Ada Tremblay ", ContactEmail: "a@example.test"}, nil},
		{"empty name", intake.Submission{FormID: "f1", ContactName: "   "}, party.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := partyfake.New(fixedNow)
			c, err := party.CreateFromSubmission(ctx, store, tt.sub, "user-1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if c.Kind != party.Person || c.LegalName != "Ada Tremblay" || c.DisplayName != "Ada Tremblay" {
				t.Fatalf("contact = %+v", c)
			}
			got, err := store.Get(ctx, c.ID)
			if err != nil || got.ID != c.ID {
				t.Fatalf("party store does not hold the intake contact: %v", err)
			}
		})
	}
}

// rule: ARC-I-040
func TestARC_I_040_ConversionKeepsContactID(t *testing.T) {
	ctx := context.Background()
	store := partyfake.New(fixedNow)
	created, err := party.CreateFromSubmission(ctx, store, intake.Submission{ContactName: "Ada Tremblay"}, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		store   party.Contacts
		lead    intake.Lead
		want    party.ContactID
		wantErr error
	}{
		{"keeps lead contact", store, intake.Lead{ID: "lead-1", ContactID: string(created.ID)}, created.ID, nil},
		{"no contact", store, intake.Lead{ID: "lead-2"}, "", party.ErrInvalid},
		{"unknown contact", store, intake.Lead{ID: "lead-3", ContactID: "contact-99"}, "", party.ErrNotFound},
		{"merged contact", mergedStore{store}, intake.Lead{ID: "lead-4", ContactID: string(created.ID)}, "", party.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := party.ClientOfLead(ctx, tt.store, tt.lead)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("client = %q, want %q", got, tt.want)
			}
		})
	}
}
