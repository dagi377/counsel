// Package partyfake is an in-memory app/legal/party for tests: no network, no Postgres.
package partyfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/party"
)

var _ party.Contacts = (*Fake)(nil)

// Fake stores contacts in memory.
type Fake struct {
	mu       sync.Mutex
	now      func() time.Time
	next     int
	contacts map[party.ContactID]party.Contact
}

// New returns an empty fake whose timestamps come from now.
func New(now func() time.Time) *Fake {
	return &Fake{now: now, contacts: map[party.ContactID]party.Contact{}}
}

// Create implements party.Contacts.
func (f *Fake) Create(ctx context.Context, c party.NewContact) (party.Contact, error) {
	if err := ctx.Err(); err != nil {
		return party.Contact{}, fmt.Errorf("create contact: %w", err)
	}
	if c.Kind != party.Person && c.Kind != party.Organization {
		return party.Contact{}, fmt.Errorf("create contact: kind %q: %w", c.Kind, party.ErrInvalid)
	}
	if c.LegalName == "" || c.DisplayName == "" {
		return party.Contact{}, fmt.Errorf("create contact: name required: %w", party.ErrInvalid)
	}
	lang := c.PreferredLanguage
	if lang == "" {
		lang = "en"
	}
	if lang != "en" && lang != "fr" {
		return party.Contact{}, fmt.Errorf("create contact: language %q: %w", lang, party.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	out := party.Contact{
		ID:                party.ContactID("contact-" + strconv.Itoa(f.next)),
		Kind:              c.Kind,
		LegalName:         c.LegalName,
		DisplayName:       c.DisplayName,
		PreferredLanguage: lang,
		IsClient:          c.IsClient,
		State:             party.Proposed,
		CreatedAt:         f.now(),
		RowVersion:        1,
	}
	f.contacts[out.ID] = out
	return out, nil
}

// Get implements party.Contacts.
func (f *Fake) Get(ctx context.Context, id party.ContactID) (party.Contact, error) {
	if err := ctx.Err(); err != nil {
		return party.Contact{}, fmt.Errorf("get contact: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.contacts[id]
	if !ok {
		return party.Contact{}, fmt.Errorf("get contact %s: %w", id, party.ErrNotFound)
	}
	return c, nil
}
