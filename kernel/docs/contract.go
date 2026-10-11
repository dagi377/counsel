// CONTRACT: kernel/docs. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-032 subtasks (kind.go, upload.go, text.go,
// cite.go, blob.go). Source: spec/eng/k1-kernel.html (kernel/docs).
//
// Document text and bytes never reach logs, errors, events or audit detail.

package docs

import (
	"context"
	"errors"
	"github.com/dagi377/counsel/kernel/registry"
	"io"

	"github.com/jackc/pgx/v5"
)

// Subject is an opaque reference to the business object a document belongs to. The
// kernel never interprets it; it mirrors registry.Subject.
type Subject = registry.Subject

// VersionID is one stored version of a document.
type VersionID string

// Classification is how sensitive a document kind is.
type Classification string

// Classifications.
const (
	Public       Classification = "public"
	Internal     Classification = "internal"
	Confidential Classification = "confidential"
	Restricted   Classification = "restricted"
)

// Kind is a document kind the app registers at boot.
type Kind struct {
	ID             string // module-prefixed, e.g. "<module>.<kind>"
	Templates      []string
	Classification Classification
	RetentionClass string // retention.Class id
}

// Line is one line of extracted text on a page. Numbers start at 1.
type Line struct {
	Page int
	No   int
	Text string
}

// Citation points at lines of one version. QuoteHash is "sha256:" + hex SHA-256 of the cited
// lines joined by "\n" and normalised per ARC-I-037 (NFC, whitespace runs collapsed to one
// space, trimmed; see QuoteHash); it is re-checked when the citation is rendered.
type Citation struct {
	VersionID VersionID
	Page      int
	FromLine  int
	ToLine    int
	QuoteHash string
}

// Sentinel errors.
var (
	ErrUnknownKind      = errors.New("docs: unregistered document kind") // 422 at the API
	ErrDuplicateKind    = errors.New("docs: document kind already registered")
	ErrNotFound         = errors.New("docs: version not found")
	ErrBadRange         = errors.New("docs: page or line out of range")
	ErrCitationMismatch = errors.New("docs: cited text changed")
)

// Kinds holds the registered document kinds (T-032.1).
type Kinds interface {
	RegisterKind(k Kind) error
	Kind(id string) (Kind, error)
}

// Store stores versions and cites their text (T-032.1, T-032.2, T-032.3).
type Store interface {
	// Put stores a new version of a document of a registered kind.
	Put(ctx context.Context, tx pgx.Tx, kind string, subject Subject, r io.Reader) (VersionID, error)
	// Lines returns the text lines of one page of a version.
	Lines(ctx context.Context, v VersionID, page int) ([]Line, error)
	// Cite returns a citation for lines l0..l1 (inclusive) of a page.
	Cite(ctx context.Context, v VersionID, page, l0, l1 int) (Citation, error)
	// Verify re-hashes the cited lines; ErrCitationMismatch when they no longer match.
	Verify(ctx context.Context, c Citation) error
}

// Service is everything kernel/docs provides.
type Service interface {
	Kinds
	Store
}
