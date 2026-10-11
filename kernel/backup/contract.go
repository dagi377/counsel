// CONTRACT: kernel/backup (T-043). Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Source: spec/eng/a1-uptime (ARC-U-042: the month-end export object
// exists in the DR bucket before its database row commits; ARC-U-043: restore drills run every
// registered restore check). Bodies live in the T-043 subtasks (export.go).

package backup

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
)

// Month is one calendar month in the business time zone.
type Month struct {
	Year  int
	Month time.Month
}

// ObjectRef describes one stored object in the DR bucket.
type ObjectRef struct {
	Bucket string
	Key    string
	SHA256 string // hex
	Size   int64
}

// ExportRecord is the database row for a month-end export. It commits only after Object
// is readable in the bucket.
type ExportRecord struct {
	Month  Month
	Object ObjectRef
}

// CheckFunc is a registered restore check, run against a restored database.
type CheckFunc func(ctx context.Context, tx pgx.Tx) error

// CheckResult is the outcome of one restore check.
type CheckResult struct {
	Name string
	Err  error
}

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrNotFound means the object or export does not exist.
	ErrNotFound = errors.New("backup: not found")
	// ErrObjectMissing means a record was written before its object reached the bucket.
	ErrObjectMissing = errors.New("backup: export object missing")
	// ErrAlreadyExported means the month already has an export.
	ErrAlreadyExported = errors.New("backup: month already exported")
	// ErrDuplicateCheck means a restore check name is registered twice.
	ErrDuplicateCheck = errors.New("backup: duplicate restore check")
	// ErrCheckFailed means at least one restore check failed.
	ErrCheckFailed = errors.New("backup: restore check failed")
)

// ObjectStore is the DR bucket.
type ObjectStore interface {
	Put(ctx context.Context, key string, r io.Reader) (ObjectRef, error)
	Head(ctx context.Context, key string) (ObjectRef, error)
}

// Exporter writes a month-end export: object first, then the record.
type Exporter interface {
	ExportMonth(ctx context.Context, m Month, r io.Reader) (ExportRecord, error)
	Export(ctx context.Context, m Month) (ExportRecord, error)
}

// RestoreChecks holds the checks registered at boot and runs them after a restore.
type RestoreChecks interface {
	Register(name string, fn CheckFunc) error
	Run(ctx context.Context, tx pgx.Tx) ([]CheckResult, error)
}
