package intake

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/party/partyfake"
)

type memForms map[string]Form

func (m memForms) Form(_ context.Context, id string) (Form, error) {
	f, ok := m[id]
	if !ok {
		return Form{}, ErrNotFound
	}
	return f, nil
}

type memLeads struct {
	mu    sync.Mutex
	seq   int
	leads []Lead
	byKey map[string]Lead
}

func (m *memLeads) ByIdempotencyKey(_ context.Context, formID, key string) (Lead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byKey[formID+"\x00"+key]
	if !ok {
		return Lead{}, ErrNotFound
	}
	return l, nil
}

func (m *memLeads) Insert(_ context.Context, l Lead, key string) (Lead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key != "" {
		if prev, ok := m.byKey[l.FormID+"\x00"+key]; ok {
			return prev, nil
		}
	}
	m.seq++
	l.ID = "lead-" + strconv.Itoa(m.seq)
	m.leads = append(m.leads, l)
	if key != "" {
		m.byKey[l.FormID+"\x00"+key] = l
	}
	return l, nil
}

func TestSubmit(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	valid := Submission{FormID: "f-pub", ContactName: "A Person", ContactEmail: "a@example.test", IdempotencyKey: "k1"}
	tests := []struct {
		name      string
		subs      []Submission
		wantErr   error
		wantLeads int
	}{
		{"published form creates lead", []Submission{valid}, nil, 1},
		{"duplicate submission creates one lead", []Submission{valid, valid}, nil, 1},
		{"different keys create two leads", []Submission{valid, func() Submission { s := valid; s.IdempotencyKey = "k2"; return s }()}, nil, 2},
		{"unpublished form refused", []Submission{func() Submission { s := valid; s.FormID = "f-draft"; return s }()}, ErrFormNotPublished, 0},
		{"unknown form", []Submission{func() Submission { s := valid; s.FormID = "nope"; return s }()}, ErrNotFound, 0},
		{"missing name", []Submission{func() Submission { s := valid; s.ContactName = " "; return s }()}, ErrInvalid, 0},
		{"no email or phone", []Submission{func() Submission { s := valid; s.ContactEmail = ""; return s }()}, ErrInvalid, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			contacts := partyfake.New(func() time.Time { return now })
			store := &memLeads{byKey: map[string]Lead{}}
			forms := memForms{"f-pub": {ID: "f-pub", Published: true}, "f-draft": {ID: "f-draft"}}
			svc := NewLeads(forms, store, contacts, func() time.Time { return now })
			var first Lead
			for i, s := range tt.subs {
				l, err := svc.Submit(ctx, s)
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Submit #%d err = %v, want %v", i, err, tt.wantErr)
				}
				if err != nil {
					continue
				}
				if i == 0 {
					first = l
				} else if s.IdempotencyKey == tt.subs[0].IdempotencyKey && l.ID != first.ID {
					t.Errorf("duplicate submission returned lead %s, want %s", l.ID, first.ID)
				}
				if l.Status != LeadOpen || l.FormID != s.FormID || !l.CreatedAt.Equal(now) {
					t.Errorf("lead = %+v", l)
				}
				c, err := contacts.Get(ctx, party.ContactID(l.ContactID))
				if err != nil {
					t.Fatalf("lead contact %q: %v", l.ContactID, err)
				}
				if c.Kind != party.Person {
					t.Errorf("contact kind = %q, want person", c.Kind)
				}
			}
			if len(store.leads) != tt.wantLeads {
				t.Errorf("stored leads = %d, want %d", len(store.leads), tt.wantLeads)
			}
		})
	}
}
