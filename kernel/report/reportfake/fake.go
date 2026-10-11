// Package reportfake is an in-memory report.Engine for tests: no network, Postgres or
// Docker. Params schemas are not validated; params only have to be a JSON object.
package reportfake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/report"
	"github.com/jackc/pgx/v5"
)

var _ report.Engine = (*Fake)(nil)

var idRe = regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)

// Caps is an in-memory report.Authorizer: user id -> capability ids held.
type Caps map[string][]string

// Can reports whether userID holds capability.
func (c Caps) Can(_ context.Context, userID, capability string) (bool, error) {
	for _, h := range c[userID] {
		if h == capability {
			return true, nil
		}
	}
	return false, nil
}

type slotKey struct {
	schedule string
	slot     int64
}

// Fake is an in-memory report engine.
type Fake struct {
	mu        sync.Mutex
	now       func() time.Time
	auth      report.Authorizer
	defs      map[string]report.ReportDef
	schedules map[string]report.Schedule
	slots     map[slotKey]report.Run
	runs      []report.Run
	seq       int
}

// New returns an empty engine reading time from now and checking capabilities with auth.
func New(now func() time.Time, auth report.Authorizer) *Fake {
	return &Fake{now: now, auth: auth, defs: map[string]report.ReportDef{}, schedules: map[string]report.Schedule{}, slots: map[slotKey]report.Run{}}
}

// Register adds a report definition.
func (f *Fake) Register(d report.ReportDef) error {
	if !idRe.MatchString(d.ID) || d.Capability == "" || d.Run == nil {
		return fmt.Errorf("register %q: %w", d.ID, report.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[d.ID]; ok {
		return fmt.Errorf("register %q: %w", d.ID, report.ErrDuplicate)
	}
	f.defs[d.ID] = d
	return nil
}

// ParamsHash is "sha256:" + hex of params re-encoded with sorted keys and no whitespace.
func ParamsHash(params json.RawMessage) (string, error) {
	var v map[string]any
	if err := json.Unmarshal(params, &v); err != nil {
		return "", fmt.Errorf("params: %w", report.ErrInvalidParams)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("params: %w", report.ErrInvalidParams)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// execute runs d for user by and records the run. The caller holds f.mu.
func (f *Fake) execute(ctx context.Context, tx pgx.Tx, d report.ReportDef, params json.RawMessage, by string) (report.Run, report.Rows, error) {
	ok, err := f.auth.Can(ctx, by, d.Capability)
	if err != nil {
		return report.Run{}, report.Rows{}, fmt.Errorf("run %s: authorize: %w", d.ID, err)
	}
	if !ok {
		return report.Run{}, report.Rows{}, fmt.Errorf("run %s: %w", d.ID, report.ErrForbidden)
	}
	hash, err := ParamsHash(params)
	if err != nil {
		return report.Run{}, report.Rows{}, fmt.Errorf("run %s: %w", d.ID, err)
	}
	started := f.now()
	rows, err := d.Run(ctx, tx, params)
	if err != nil {
		return report.Run{}, report.Rows{}, fmt.Errorf("run %s: %w", d.ID, err)
	}
	f.seq++
	r := report.Run{
		ID: report.RunID(fmt.Sprintf("run-%d", f.seq)), ReportID: d.ID, ParamsHash: hash,
		RowCount: len(rows.Rows), By: by, StartedAt: started, FinishedAt: f.now(),
	}
	f.runs = append(f.runs, r)
	return r, rows, nil
}

// Run runs a report for user by.
func (f *Fake) Run(ctx context.Context, tx pgx.Tx, reportID string, params json.RawMessage, by string) (report.Run, report.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.defs[reportID]
	if !ok {
		return report.Run{}, report.Rows{}, fmt.Errorf("run %q: %w", reportID, report.ErrNotFound)
	}
	return f.execute(ctx, tx, d, params, by)
}

// AddSchedule stores a schedule.
func (f *Fake) AddSchedule(_ context.Context, _ pgx.Tx, s report.Schedule) error {
	if s.ID == "" || s.Every <= 0 || s.Owner == "" {
		return fmt.Errorf("schedule %q: %w", s.ID, report.ErrInvalid)
	}
	if _, err := ParamsHash(s.Params); err != nil {
		return fmt.Errorf("schedule %q: %w", s.ID, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[s.ReportID]; !ok {
		return fmt.Errorf("schedule %q: report: %w", s.ID, report.ErrNotFound)
	}
	if _, ok := f.schedules[s.ID]; ok {
		return fmt.Errorf("schedule %q: %w", s.ID, report.ErrDuplicate)
	}
	f.schedules[s.ID] = s
	return nil
}

// RunSlot runs a schedule once for the slot containing at.
func (f *Fake) RunSlot(ctx context.Context, tx pgx.Tx, scheduleID string, at time.Time) (report.Run, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.schedules[scheduleID]
	if !ok {
		return report.Run{}, false, fmt.Errorf("run slot %q: %w", scheduleID, report.ErrNotFound)
	}
	n := int64(at.Sub(s.Anchor) / s.Every)
	if at.Before(s.Anchor) && at.Sub(s.Anchor)%s.Every != 0 {
		n--
	}
	key := slotKey{schedule: scheduleID, slot: n}
	if r, done := f.slots[key]; done {
		return r, false, nil
	}
	r, _, err := f.execute(ctx, tx, f.defs[s.ReportID], s.Params, s.Owner)
	if err != nil {
		return report.Run{}, false, fmt.Errorf("run slot %q: %w", scheduleID, err)
	}
	r.By, r.ScheduleID, r.Slot = "", scheduleID, s.Anchor.Add(time.Duration(n)*s.Every)
	for i := range f.runs {
		if f.runs[i].ID == r.ID {
			f.runs[i] = r
		}
	}
	f.slots[key] = r
	return r, true, nil
}

// Export renders rows as CSV, or as a minimal text PDF, with a sha256 manifest.
func (f *Fake) Export(_ context.Context, run report.Run, rows report.Rows, format report.Format) (report.Export, error) {
	var buf bytes.Buffer
	switch format {
	case report.FormatCSV:
		w := csv.NewWriter(&buf)
		if err := w.Write(rows.Columns); err != nil {
			return report.Export{}, fmt.Errorf("export csv: %w", err)
		}
		if err := w.WriteAll(rows.Rows); err != nil {
			return report.Export{}, fmt.Errorf("export csv: %w", err)
		}
	case report.FormatPDF:
		buf.WriteString("%PDF-1.4\n")
		buf.WriteString(strings.Join(rows.Columns, "\t") + "\n")
		for _, r := range rows.Rows {
			buf.WriteString(strings.Join(r, "\t") + "\n")
		}
		buf.WriteString("%%EOF\n")
	default:
		return report.Export{}, fmt.Errorf("export %q: %w", format, report.ErrFormat)
	}
	sum := sha256.Sum256(buf.Bytes())
	return report.Export{
		Content: buf.Bytes(),
		Manifest: report.Manifest{
			RunID: run.ID, ReportID: run.ReportID, Format: format, SHA256: hex.EncodeToString(sum[:]),
			Bytes: buf.Len(), RowCount: len(rows.Rows),
		},
	}, nil
}

// Runs lists a report's runs in start order.
func (f *Fake) Runs(_ context.Context, reportID string) ([]report.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[reportID]; !ok {
		return nil, fmt.Errorf("runs %q: %w", reportID, report.ErrNotFound)
	}
	var out []report.Run
	for _, r := range f.runs {
		if r.ReportID == reportID {
			out = append(out, r)
		}
	}
	return out, nil
}
