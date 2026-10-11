// CONTRACT: app/legal/party. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-010b subtasks (contact.go).
// Source: migrations/0001_init.sql (party.contact).

package party

import (
	"context"
	"errors"
	"time"
)

// ContactID is a party.contact id.
type ContactID string

// Kind is a contact's kind.
type Kind string

// Contact kinds.
const (
	Person       Kind = "person"
	Organization Kind = "organization"
)

// State is a contact's lifecycle state.
type State string

// Contact states.
const (
	Proposed State = "proposed"
	Active   State = "active"
	Archived State = "archived"
	Merged   State = "merged"
)

// NewContact is what Create needs. Names are client data: never log them.
type NewContact struct {
	Kind              Kind
	LegalName         string
	DisplayName       string
	PreferredLanguage string // "en" (default) or "fr"
	IsClient          bool
	CreatedBy         string // core.app_user id
}

// Contact is one party.contact row as the module API returns it.
type Contact struct {
	ID                ContactID
	Kind              Kind
	LegalName         string
	DisplayName       string
	PreferredLanguage string
	IsClient          bool
	State             State
	CreatedAt         time.Time
	RowVersion        int
}

// Sentinel errors.
var (
	ErrNotFound = errors.New("party: contact not found")
	ErrInvalid  = errors.New("party: invalid contact")
)

// Contacts creates and reads contacts (T-010b.2). Other modules read contacts only
// through this API.
type Contacts interface {
	Create(ctx context.Context, c NewContact) (Contact, error)
	Get(ctx context.Context, id ContactID) (Contact, error)
	// Search returns the contacts, merged ones excluded, whose legal or display name contains
	// query (case-insensitive), by display name then id, at most SearchLimit. An empty query
	// matches every contact. Added for the search_contacts agent tool (FEAT-TOOL-02).
	Search(ctx context.Context, query string) ([]Contact, error)
	// List returns one page of contacts, merged ones excluded, by id, starting after
	// Filter.After. Added for GET /contacts (HTTP-03).
	List(ctx context.Context, f Filter) ([]Contact, error)
}

// SearchLimit caps how many contacts one Search returns.
const SearchLimit = 200
