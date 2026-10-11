package intake

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

type memLeadReader map[string]Lead

func (m memLeadReader) Lead(_ context.Context, id string) (Lead, error) {
	l, ok := m[id]
	if !ok {
		return Lead{}, ErrNotFound
	}
	return l, nil
}

type memConsults struct {
	mu    sync.Mutex
	seq   int
	order []string
	byID  map[string]Consultation
}

func newMemConsults() *memConsults { return &memConsults{byID: map[string]Consultation{}} }

func (m *memConsults) InsertConsultation(_ context.Context, c Consultation) (Consultation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	c.ID = "consult-" + strconv.Itoa(m.seq)
	m.byID[c.ID] = c
	m.order = append(m.order, c.ID)
	return c, nil
}

func (m *memConsults) Consultation(_ context.Context, id string) (Consultation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return Consultation{}, ErrNotFound
	}
	return c, nil
}

func (m *memConsults) SetOutcome(_ context.Context, id string, o Outcome) (Consultation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.byID[id]
	if !ok {
		return Consultation{}, ErrNotFound
	}
	c.Outcome = o
	m.byID[id] = c
	return c, nil
}

func (m *memConsults) ByLead(_ context.Context, leadID string) ([]Consultation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Consultation
	for _, id := range m.order {
		if c := m.byID[id]; c.LeadID == leadID {
			out = append(out, c)
		}
	}
	return out, nil
}

func consultFixture() (memLeadReader, *memConsults, *Consultations) {
	leads := memLeadReader{
		"lead-open":     {ID: "lead-open", Status: LeadOpen},
		"lead-declined": {ID: "lead-declined", Status: LeadSealedProspect},
		"lead-done":     {ID: "lead-done", Status: LeadConverted},
	}
	store := newMemConsults()
	return leads, store, NewConsultations(leads, store)
}

func TestBook(t *testing.T) {
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		leadID   string
		with     string
		startsAt time.Time
		wantErr  error
	}{
		{"open lead books", "lead-open", "user-1", at, nil},
		{"missing lead id", "", "user-1", at, ErrInvalid},
		{"missing with", "lead-open", " ", at, ErrInvalid},
		{"missing start", "lead-open", "user-1", time.Time{}, ErrInvalid},
		{"unknown lead", "lead-x", "user-1", at, ErrNotFound},
		{"sealed lead", "lead-declined", "user-1", at, ErrLeadClosed},
		{"converted lead", "lead-done", "user-1", at, ErrLeadClosed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, store, svc := consultFixture()
			c, err := svc.Book(context.Background(), tc.leadID, tc.with, tc.startsAt)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Book err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(store.order) != 0 {
					t.Fatalf("stored %d consultations on error, want 0", len(store.order))
				}
				return
			}
			if c.ID == "" || c.LeadID != tc.leadID || c.With != tc.with || !c.StartsAt.Equal(tc.startsAt) || c.Outcome != OutcomeNone {
				t.Fatalf("Book = %+v", c)
			}
		})
	}
}

func TestRecordOutcome(t *testing.T) {
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		closeLead bool
		id        string // "" means the booked consultation
		outcome   Outcome
		wantErr   error
	}{
		{"retain", false, "", OutcomeRetain, nil},
		{"decline", false, "", OutcomeDecline, nil},
		{"follow up", false, "", OutcomeFollowUp, nil},
		{"none rejected", false, "", OutcomeNone, ErrInvalid},
		{"unknown outcome rejected", false, "", Outcome("maybe"), ErrInvalid},
		{"unknown consultation", false, "consult-x", OutcomeRetain, ErrNotFound},
		{"lead closed after booking", true, "", OutcomeRetain, ErrLeadClosed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			leads, store, svc := consultFixture()
			ctx := context.Background()
			booked, err := svc.Book(ctx, "lead-open", "user-1", at)
			if err != nil {
				t.Fatal(err)
			}
			if tc.closeLead {
				leads["lead-open"] = Lead{ID: "lead-open", Status: LeadConverted}
			}
			id := tc.id
			if id == "" {
				id = booked.ID
			}
			got, err := svc.RecordOutcome(ctx, id, tc.outcome)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("RecordOutcome err = %v, want %v", err, tc.wantErr)
			}
			stored := store.byID[booked.ID]
			if tc.wantErr != nil {
				if stored.Outcome != OutcomeNone {
					t.Fatalf("outcome stored on error: %q", stored.Outcome)
				}
				return
			}
			if got.Outcome != tc.outcome || stored.Outcome != tc.outcome {
				t.Fatalf("outcome = %q (stored %q), want %q", got.Outcome, stored.Outcome, tc.outcome)
			}
		})
	}
}

// The outcome is required before conversion: RequireOutcome passes only once a consultation
// booked against the lead has an outcome.
func TestRequireOutcome(t *testing.T) {
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		bookings int
		outcomes []Outcome // per booking; OutcomeNone leaves it unrecorded
		wantErr  error
	}{
		{"no consultation", 0, nil, ErrOutcomeRequired},
		{"booked, no outcome", 1, []Outcome{OutcomeNone}, ErrOutcomeRequired},
		{"booked with outcome", 1, []Outcome{OutcomeRetain}, nil},
		{"second consultation has outcome", 2, []Outcome{OutcomeNone, OutcomeFollowUp}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, svc := consultFixture()
			ctx := context.Background()
			for i := 0; i < tc.bookings; i++ {
				c, err := svc.Book(ctx, "lead-open", "user-1", at.Add(time.Duration(i)*time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if o := tc.outcomes[i]; o != OutcomeNone {
					if _, err := svc.RecordOutcome(ctx, c.ID, o); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := svc.RequireOutcome(ctx, "lead-open"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("RequireOutcome err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
