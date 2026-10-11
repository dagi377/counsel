// CONTRACT: app/legal/docs. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-032L subtasks (kinds.go, folders.go,
// destruction.go).
// Source: spec/eng/k1-kernel.html (Documents row), spec/legal/documents.json.

// Package docs holds the legal side of documents: the document kinds and retention classes
// Counsel registers with kernel/docs, matter folder trees, and file destruction proposals.
package docs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Document kind ids registered with the kernel.
const (
	KindCourtForm        = "court_form"
	KindEngagementLetter = "engagement_letter"
	KindPrecedent        = "precedent"
	KindResearchMemo     = "research_memo"
	KindBundle           = "bundle"
	KindAnalysisTable    = "analysis_table"
)

// Retention class ids with fixed behaviour. RC-FIN and RC-AUDIT are never purged in v1.
const (
	RetentionFinancial = "RC-FIN"
	RetentionAudit     = "RC-AUDIT"
)

// DestructionGate is the gate kind that approves a file destruction (spec gate G14).
const DestructionGate = "platform.file_destruction"

// FolderID identifies a folder in a matter's folder tree.
type FolderID string

// DocumentKind is a legal document kind and its retention class.
type DocumentKind struct {
	ID             string
	Description    string
	RetentionClass string
}

// RetentionClass says how long records of a class are kept after the matter closes.
type RetentionClass struct {
	ID              string
	MinYears        int
	NeverPurge      bool
	DestructionGate string
}

// Folder is one folder in a matter's tree. ParentID is empty for a top-level folder.
type Folder struct {
	ID       FolderID
	MatterID string
	ParentID FolderID
	Name     string
}

// FolderTemplate is the default folder tree for one practice area. Each path is
// slash-separated ("Pleadings/Drafts"); parents are created before children.
type FolderTemplate struct {
	PracticeArea string
	Paths        []string
}

// Document is a filed document as this package sees it.
type Document struct {
	ID       string
	MatterID string
	Kind     string
	FolderID FolderID
}

// Proposal is a file destruction proposal waiting on its gate.
type Proposal struct {
	MatterID    string
	GateID      string
	DocumentIDs []string // documents whose retention has run out
	ProposedOn  time.Time
}

// DestructionGates opens the destruction gate. kernel/gate implements it through the
// app's gate wiring; tests use a fake.
type DestructionGates interface {
	OpenDestruction(ctx context.Context, tx pgx.Tx, matterID string, documentIDs []string, preparer string) (gateID string, err error)
}

// Catalog lists the kinds and retention classes the app registers.
type Catalog interface {
	Kinds() []DocumentKind
	RetentionClasses() []RetentionClass
}

// Folders manages matter folder trees and filing.
type Folders interface {
	// CreateMatterFolders seeds a new matter's tree from its practice area's template.
	// Calling it again for the same matter returns the existing tree.
	CreateMatterFolders(ctx context.Context, tx pgx.Tx, matterID, practiceArea string) ([]Folder, error)
	// Folders lists a matter's folders, parents first.
	Folders(ctx context.Context, matterID string) ([]Folder, error)
	// File puts a document in exactly one folder of its matter; filing again moves it.
	File(ctx context.Context, tx pgx.Tx, d Document) error
	// Document returns one filed document.
	Document(ctx context.Context, id string) (Document, error)
}

// Destruction proposes purges of closed matters' files.
type Destruction interface {
	// ProposeDestruction lists the documents of a closed matter whose retention has run
	// out on today and opens DestructionGate for them. Nothing is purged until that gate is
	// approved by a lawyer.
	ProposeDestruction(ctx context.Context, tx pgx.Tx, matterID string, closedOn, today time.Time, preparer string) (Proposal, error)
}

// Service is the whole legal docs surface.
type Service interface {
	Catalog
	Folders
	Destruction
}

// Sentinel errors.
var (
	ErrNotFound       = errors.New("docs: not found")
	ErrInvalid        = errors.New("docs: invalid input")
	ErrUnknownKind    = errors.New("docs: unknown document kind")
	ErrNoTemplate     = errors.New("docs: no folder template for practice area")
	ErrWrongMatter    = errors.New("docs: folder belongs to another matter")
	ErrMatterOpen     = errors.New("docs: matter not closed")
	ErrNothingToPurge = errors.New("docs: nothing past retention")
)
