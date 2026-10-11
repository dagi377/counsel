// CONTRACT: app/legal/comms types and interfaces. Frozen once on main; changes are additive
// only and need a docs/QUESTIONS.md entry. Sources: spec/legal (portal and comms screens),
// table comms.comm_filing.

// Package comms holds the legal side of client communication: client portal page loaders,
// court filing records and message templates for legal notices.
package comms

import (
	"context"
	"errors"
	"time"
)

// Guest is a portal visitor: a client contact who sees only their own matters.
type Guest struct {
	ContactID string
	MatterIDs []string
}

// ItemKind is the kind of item shared to the portal.
type ItemKind string

const (
	ItemDocument ItemKind = "document"
	ItemBill     ItemKind = "bill"
)

// SharedItem is a document or bill a firm user shared to the portal for one matter.
type SharedItem struct {
	ID       string
	MatterID string
	Kind     ItemKind
	Title    string
	SharedBy string
	SharedAt time.Time
}

// Portal loads what a guest may see. Items outside the guest's matters do not exist for them.
type Portal interface {
	// Share makes item visible on the portal for its matter.
	Share(ctx context.Context, item SharedItem) error
	// Matters lists the guest's matter ids that have at least one shared item, sorted.
	Matters(ctx context.Context, g Guest) ([]string, error)
	// Items lists shared items of one of the guest's matters, oldest first; a matter the
	// guest is not on returns ErrNotFound.
	Items(ctx context.Context, g Guest, matterID string) ([]SharedItem, error)
}

// FilingStatus is the state of a court filing.
type FilingStatus string

const (
	FilingDraft    FilingStatus = "draft"
	FilingFiled    FilingStatus = "filed"
	FilingRejected FilingStatus = "rejected"
)

// Filing is one court filing record (comms.comm_filing).
type Filing struct {
	ID         string
	MatterID   string
	DocumentID string
	Court      string
	Reference  string // court file or e-filing reference
	Status     FilingStatus
	FiledBy    string
	FiledOn    time.Time // business date
	RecordedAt time.Time
}

// Filings records court filings.
type Filings interface {
	// RecordFiling appends f and returns it with ID and RecordedAt set.
	RecordFiling(ctx context.Context, f Filing) (Filing, error)
	// FilingsFor lists a matter's filings, oldest first.
	FilingsFor(ctx context.Context, matterID string) ([]Filing, error)
}

// Template is a message template for a legal notice. Subject and Body use {{name}} placeholders.
type Template struct {
	ID      string
	Name    string
	Subject string
	Body    string
}

// Templates stores and renders message templates.
type Templates interface {
	// PutTemplate adds or replaces t.
	PutTemplate(ctx context.Context, t Template) error
	// Render fills every placeholder of template id from vars; a missing var returns ErrMissingVar.
	Render(ctx context.Context, id string, vars map[string]string) (subject, body string, err error)
}

// Service is the whole comms surface.
type Service interface {
	Portal
	Filings
	Templates
}

var (
	// ErrNotFound is returned for an unknown id or an item outside the guest's matters.
	ErrNotFound = errors.New("comms: not found")
	// ErrInvalid is returned for a record missing a required field.
	ErrInvalid = errors.New("comms: invalid record")
	// ErrMissingVar is returned when a template placeholder has no value.
	ErrMissingVar = errors.New("comms: missing template variable")
)
