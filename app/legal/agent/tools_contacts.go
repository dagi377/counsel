package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolSearchContacts finds contacts by name among the matters the acting user can see.
const ToolSearchContacts = "search_contacts"

// CapContactsView is the capability search_contacts needs.
const CapContactsView = "contacts.view"

// ContactSearch finds contacts by name (app/legal/party Contacts.Search).
type ContactSearch interface {
	Search(ctx context.Context, query string) ([]party.Contact, error)
}

// ClientMatters lists matters, here by client (app/legal/matter Query.List).
type ClientMatters interface {
	List(ctx context.Context, f matter.Filter) ([]matter.Matter, error)
}

// buildSearchContacts reads Deps: CapabilityCheck, roles.Walls, ContactSearch, ClientMatters.
func buildSearchContacts(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
	}
	contacts, err := DepOf[ContactSearch](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
	}
	matters, err := DepOf[ClientMatters](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
	}
	s := searchContacts{walls: walls, contacts: contacts, matters: matters}
	return handlerTool(ToolSearchContacts, Read, can, s.handle)
}

type searchContacts struct {
	walls    roles.Walls
	contacts ContactSearch
	matters  ClientMatters
}

// contactOut is one found contact: ids and flags only. Its name is client text and travels
// as quotes[i], the quoted envelope at the same index.
type contactOut struct {
	ContactID string   `json:"contact_id"`
	Kind      string   `json:"kind"`
	IsClient  bool     `json:"is_client"`
	State     string   `json:"state"`
	MatterIDs []string `json:"matter_ids"` // the visible matters the contact is client of
}

type searchContactsOut struct {
	Contacts   []contactOut        `json:"contacts"`
	Quotes     []agenttools.Quoted `json:"quotes"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// ContactCitation cites a contact record. A contact has no document version, so the record
// id stands in for the version, at page 1, line 1.
func ContactCitation(id party.ContactID) agenttools.Citation {
	return agenttools.Citation{VersionID: "contact:" + string(id), Page: 1, FromLine: 1, ToLine: 1}
}

func (s searchContacts) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		Query  string `json:"query"`
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchContacts, ErrInvalidInput)
	}
	offset, n, err := pageWindow(in.Cursor, in.Limit)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
	}
	found, err := s.contacts.Search(ctx, in.Query)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: contacts: %w", ToolSearchContacts, err)
	}
	var match []party.Contact
	var visible [][]string
	for _, c := range found {
		ids, err := s.visibleMatters(ctx, user, c.ID)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
		}
		// rule: ARC-I-036
		// A contact of walled matters only is omitted, not reported as hidden.
		if len(ids) == 0 {
			continue
		}
		match = append(match, c)
		visible = append(visible, ids)
	}
	out := searchContactsOut{Contacts: []contactOut{}, Quotes: []agenttools.Quoted{}, NextCursor: nextCursor(offset, n, len(match))}
	var cites []agenttools.Citation
	for i := offset; i < min(offset+n, len(match)); i++ {
		c := match[i]
		out.Contacts = append(out.Contacts, contactOut{
			ContactID: string(c.ID), Kind: string(c.Kind), IsClient: c.IsClient, State: string(c.State), MatterIDs: visible[i],
		})
		// rule: ARC-I-038
		// The name is client text: it goes back quoted as untrusted data with its citation.
		src := ContactCitation(c.ID)
		out.Quotes = append(out.Quotes, agenttools.Quote(src, c.DisplayName))
		cites = append(cites, src)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchContacts, err)
	}
	return registry.ToolResult{Output: b, Citations: cites}, nil
}

// visibleMatters returns the matters contact id is client of that user can see.
func (s searchContacts) visibleMatters(ctx context.Context, user string, id party.ContactID) ([]string, error) {
	ms, err := s.matters.List(ctx, matter.Filter{ClientID: id})
	if err != nil {
		return nil, fmt.Errorf("matters: %w", err)
	}
	var out []string
	for _, m := range ms {
		err := visibleTo(ctx, s.walls, user, string(m.ID))
		if errors.Is(err, ErrNotVisible) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, string(m.ID))
	}
	return out, nil
}
