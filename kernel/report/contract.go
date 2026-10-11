// CONTRACT: kernel/report. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-038 subtasks (def.go, run.go, schedule.go,
// export.go).
// Source: spec/eng/k1-kernel.html (kernel/report).

package report

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Rows is a report result: a header and string-formatted cells.
type Rows struct {
	Columns []string
	Rows    [][]string
}

// RunFunc produces a report's rows inside tx.
type RunFunc func(ctx context.Context, tx pgx.Tx, params json.RawMessage) (Rows, error)

// ReportDef declares one report. ID is module-prefixed ("<module>.<name>").
type ReportDef struct {
	ID         string
	Capability string // capability id needed to run it
	Params     []byte // JSON Schema for params
	Run        RunFunc
}

// RunID identifies one report run.
type RunID string

// Run records one run. It holds the params hash and row count, never row data.
type Run struct {
	ID         RunID
	ReportID   string
	ParamsHash string // "sha256:" + hex of the canonical params
	RowCount   int
	By         string // user id, or empty for a scheduled run
	ScheduleID string // set for a scheduled run
	Slot       time.Time
	StartedAt  time.Time
	FinishedAt time.Time
}

// Schedule runs a report with fixed params on a recurring slot. Each slot runs once.
type Schedule struct {
	ID       string
	ReportID string
	Params   json.RawMessage
	Every    time.Duration // slot length; slots start at multiples of Every from Anchor
	Anchor   time.Time
	Owner    string // user id whose capabilities the run uses
}

// Format is an export format.
type Format string

// Export formats.
const (
	FormatCSV Format = "csv"
	FormatPDF Format = "pdf"
)

// Manifest describes an exported file so a reader can verify it.
type Manifest struct {
	RunID    RunID
	ReportID string
	Format   Format
	SHA256   string // hex of the file's SHA-256
	Bytes    int
	RowCount int
}

// Export is a rendered file and its manifest.
type Export struct {
	Content  []byte
	Manifest Manifest
}

// Authorizer reports whether a user holds a capability.
type Authorizer interface {
	Can(ctx context.Context, userID, capability string) (bool, error)
}

// Engine registers, runs, schedules and exports reports.
type Engine interface {
	// Register adds a report definition. Duplicate or malformed ids fail.
	Register(d ReportDef) error
	// Run runs a report for user by and records the run.
	Run(ctx context.Context, tx pgx.Tx, reportID string, params json.RawMessage, by string) (Run, Rows, error)
	// AddSchedule stores a schedule.
	AddSchedule(ctx context.Context, tx pgx.Tx, s Schedule) error
	// RunSlot runs a schedule for the slot containing at. A slot that already ran returns
	// its run with created false and runs nothing.
	RunSlot(ctx context.Context, tx pgx.Tx, scheduleID string, at time.Time) (r Run, created bool, err error)
	// Export renders rows of run in format with a sha256 manifest.
	Export(ctx context.Context, run Run, rows Rows, f Format) (Export, error)
	// Runs lists the runs of a report in start order.
	Runs(ctx context.Context, reportID string) ([]Run, error)
}

// Sentinel errors.
var (
	ErrNotFound      = errors.New("report: not found")
	ErrDuplicate     = errors.New("report: duplicate id")
	ErrInvalid       = errors.New("report: invalid definition")
	ErrInvalidParams = errors.New("report: invalid params")
	ErrForbidden     = errors.New("report: missing capability")
	ErrFormat        = errors.New("report: unsupported format")
)
