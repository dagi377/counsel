package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var _ Exporter = (*MonthEndExporter)(nil)

// RecordStore holds month-end export rows. Insert returns ErrAlreadyExported when the month
// already has a row, and Get returns ErrNotFound when it has none.
type RecordStore interface {
	Insert(ctx context.Context, rec ExportRecord) error
	Get(ctx context.Context, m Month) (ExportRecord, error)
}

// MonthEndExporter writes a month-end export to the primary bucket and the DR bucket, confirms
// the DR copy, and only then commits the record that references it.
type MonthEndExporter struct {
	primary ObjectStore
	dr      ObjectStore
	records RecordStore
}

// NewExporter returns an Exporter over the primary bucket, the DR bucket and the record store.
func NewExporter(primary, dr ObjectStore, records RecordStore) *MonthEndExporter {
	return &MonthEndExporter{primary: primary, dr: dr, records: records}
}

// ExportKey is the object key of a month's export.
func ExportKey(m Month) string {
	return fmt.Sprintf("month-end/%04d-%02d.tar", m.Year, int(m.Month))
}

func validMonth(m Month) error {
	if m.Year < 1 || m.Month < time.January || m.Month > time.December {
		return fmt.Errorf("export month %d-%02d: invalid month", m.Year, int(m.Month))
	}
	return nil
}

// ExportMonth writes r as the export for m. The record commits only after the object is
// readable in the DR bucket with the same digest and size as the primary copy.
func (e *MonthEndExporter) ExportMonth(ctx context.Context, m Month, r io.Reader) (ExportRecord, error) {
	if err := validMonth(m); err != nil {
		return ExportRecord{}, err
	}
	key := ExportKey(m)
	if _, err := e.records.Get(ctx, m); err == nil {
		return ExportRecord{}, fmt.Errorf("export %s: %w", key, ErrAlreadyExported)
	} else if !isNotFound(err) {
		return ExportRecord{}, fmt.Errorf("export %s: read record: %w", key, err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return ExportRecord{}, fmt.Errorf("export %s: read body: %w", key, err)
	}
	// rule: ARC-U-042
	ref, err := e.primary.Put(ctx, key, bytes.NewReader(body))
	if err != nil {
		return ExportRecord{}, fmt.Errorf("export %s: primary put: %w", key, err)
	}
	// rule: ARC-U-042
	if _, err := e.dr.Put(ctx, key, bytes.NewReader(body)); err != nil {
		return ExportRecord{}, fmt.Errorf("export %s: dr put: %w", key, err)
	}
	// rule: ARC-U-042
	got, err := e.dr.Head(ctx, key)
	if err != nil {
		return ExportRecord{}, fmt.Errorf("export %s: dr head: %v: %w", key, err, ErrObjectMissing)
	}
	if got.SHA256 != ref.SHA256 || got.Size != ref.Size {
		return ExportRecord{}, fmt.Errorf("export %s: dr copy differs from primary: %w", key, ErrObjectMissing)
	}
	rec := ExportRecord{Month: m, Object: ref}
	if err := e.records.Insert(ctx, rec); err != nil {
		return ExportRecord{}, fmt.Errorf("export %s: insert record: %w", key, err)
	}
	return rec, nil
}

// Export returns the committed export for m.
func (e *MonthEndExporter) Export(ctx context.Context, m Month) (ExportRecord, error) {
	if err := validMonth(m); err != nil {
		return ExportRecord{}, err
	}
	rec, err := e.records.Get(ctx, m)
	if err != nil {
		return ExportRecord{}, fmt.Errorf("export %s: %w", ExportKey(m), err)
	}
	return rec, nil
}

func isNotFound(err error) bool {
	return err != nil && errors.Is(err, ErrNotFound)
}

// ReplicationLagMetric is the gauge the per-cell lag exporter publishes: the age in seconds of
// the oldest object not yet present in the DR bucket.
const ReplicationLagMetric = "backup_dr_oldest_unreplicated_seconds"

// ReplicationLagPageSeconds is the SLO-05b paging bound: an object unreplicated for 15 minutes
// pages immediately.
const ReplicationLagPageSeconds = 15 * 60

// ReplicationLagAlert renders the SLO-05b DocReplicationLag alerting rule for one cell as a
// Prometheus rule group in YAML.
func ReplicationLagAlert(cell string) (string, error) {
	if cell == "" || strings.ContainsAny(cell, "\"\n{}") {
		return "", fmt.Errorf("replication lag alert: invalid cell %q", cell)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "groups:\n")
	fmt.Fprintf(&b, "  - name: backup-dr-replication\n")
	fmt.Fprintf(&b, "    rules:\n")
	fmt.Fprintf(&b, "      - alert: DocReplicationLag\n")
	fmt.Fprintf(&b, "        expr: max(%s{cell=\"%s\"}) >= %d\n", ReplicationLagMetric, cell, ReplicationLagPageSeconds)
	fmt.Fprintf(&b, "        labels:\n")
	fmt.Fprintf(&b, "          severity: page\n")
	fmt.Fprintf(&b, "          slo: SLO-05b\n")
	fmt.Fprintf(&b, "          cell: \"%s\"\n", cell)
	fmt.Fprintf(&b, "        annotations:\n")
	fmt.Fprintf(&b, "          summary: \"Oldest object not replicated to the DR bucket is 15 minutes or older\"\n")
	return b.String(), nil
}
