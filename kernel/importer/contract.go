// CONTRACT: kernel/importer. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-040 subtasks (source.go, checkpoint.go,
// idmap.go, dryrun.go, timefix.go, cli.go).

package importer

import (
	"context"
	"errors"
)

// Sentinel errors.
var (
	// ErrUnknownSource is returned for a source id nobody registered.
	ErrUnknownSource = errors.New("importer: unknown source")
	// ErrDuplicateSource is returned when a source id is registered twice.
	ErrDuplicateSource = errors.New("importer: duplicate source")
	// ErrNoApprovedDryRun is returned when a run starts without an approved, zero-error
	// dry run on the same snapshot.
	ErrNoApprovedDryRun = errors.New("importer: no approved dry run for snapshot")
	// ErrDryRunHasErrors is returned when approving a dry run that reported errors.
	ErrDryRunHasErrors = errors.New("importer: dry run has errors")
	// ErrUnmapped is returned by Transform for a resource or field it has no mapping for.
	ErrUnmapped = errors.New("importer: unmapped")
)

// Cursor is an opaque per-resource extraction position. The empty cursor is the start.
type Cursor string

// Row is one staged external record.
type Row struct {
	Resource   string
	ExternalID string
	Fields     map[string]any
}

// Page is one batch of extracted rows. Next resumes after the page; Done is set on the last.
type Page struct {
	Rows []Row
	Next Cursor
	Done bool
}

// Command is one internal write a transform produces for a staged row.
type Command struct {
	Kind     string // registered command kind, e.g. "party.create"
	TargetID string // internal id, from the id map
	Fields   map[string]any
}

// IDMap maps external ids to internal ids. Every imported row has exactly one entry.
type IDMap interface {
	// Lookup returns the internal id for an external one.
	Lookup(ctx context.Context, source, resource, externalID string) (string, bool, error)
	// Assign returns the existing internal id, or creates one; reruns return the same id.
	Assign(ctx context.Context, source, resource, externalID string) (string, error)
}

// Source is an external system the app registers as an import source.
type Source interface {
	Resources() []string
	Extract(ctx context.Context, res string, cur Cursor) (Page, error)
	Transform(ctx context.Context, staged Row, m IDMap) ([]Command, error)
}

// Issue is one dry-run finding.
type Issue struct {
	Resource   string
	ExternalID string
	Field      string
	Message    string
}

// DryRun is the result of a dry run over one snapshot.
type DryRun struct {
	ID       string
	Source   string
	Snapshot string
	Counts   map[string]int // rows per resource
	Unmapped map[string]int // unmapped resource or "resource.field" -> count
	Errors   []Issue
	Approved bool
}

// Importer runs registered sources: checkpointed extraction, dry run, gated import.
type Importer interface {
	// Register adds a source under id.
	Register(id string, s Source) error
	// DryRun extracts and transforms the snapshot without writing and reports what it found.
	DryRun(ctx context.Context, source, snapshot string) (DryRun, error)
	// Approve marks a zero-error dry run approved (through the import gate).
	Approve(ctx context.Context, dryRunID string) error
	// Run imports the snapshot. It needs an approved dry run on the same snapshot and
	// resumes from the saved checkpoints.
	Run(ctx context.Context, source, snapshot string) error
	// Checkpoint returns the saved cursor for a resource.
	Checkpoint(ctx context.Context, source, resource string) (Cursor, error)
}
