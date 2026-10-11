package backup_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/backup"
	"github.com/dagi377/counsel/kernel/backup/backupfake"
)

// records is an in-memory RecordStore that, on Insert, asserts the object is already in the DR
// bucket, so it observes the ordering a real transaction commit would.
type records struct {
	mu       sync.Mutex
	dr       backup.ObjectStore
	rows     map[backup.Month]backup.ExportRecord
	sawInDR  bool
	inserted int
}

func newRecords(dr backup.ObjectStore) *records {
	return &records{dr: dr, rows: map[backup.Month]backup.ExportRecord{}}
}

func (r *records) Insert(ctx context.Context, rec backup.ExportRecord) error {
	_, err := r.dr.Head(ctx, rec.Object.Key)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sawInDR = err == nil
	if _, ok := r.rows[rec.Month]; ok {
		return backup.ErrAlreadyExported
	}
	r.rows[rec.Month] = rec
	r.inserted++
	return nil
}

func (r *records) Get(_ context.Context, m backup.Month) (backup.ExportRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.rows[m]
	if !ok {
		return backup.ExportRecord{}, backup.ErrNotFound
	}
	return rec, nil
}

// lossyStore accepts Put but never makes the object readable, like a DR bucket that lost it.
type lossyStore struct{ backup.ObjectStore }

func (lossyStore) Head(_ context.Context, key string) (backup.ObjectRef, error) {
	return backup.ObjectRef{}, fmt.Errorf("head %s: %w", key, backup.ErrNotFound)
}

// corruptStore returns a different digest on Head than the bytes written.
type corruptStore struct{ backup.ObjectStore }

func (c corruptStore) Head(ctx context.Context, key string) (backup.ObjectRef, error) {
	ref, err := c.ObjectStore.Head(ctx, key)
	ref.SHA256 = "00"
	return ref, err
}

func TestARC_U_042_ExportObjectInDRBeforeRecord(t *testing.T) {
	jan := backup.Month{Year: 2026, Month: time.January}
	tests := []struct {
		name      string
		dr        func(*backupfake.Fake) backup.ObjectStore
		failDRPut bool
		failPrim  bool
		month     backup.Month
		wantErr   error
		wantRow   bool
	}{
		{name: "dr copy confirmed then record", dr: asStore, month: jan, wantRow: true},
		{name: "primary put fails", dr: asStore, failPrim: true, month: jan, wantErr: errBoom},
		{name: "dr put fails", dr: asStore, failDRPut: true, month: jan, wantErr: errBoom},
		{name: "dr head missing", dr: func(f *backupfake.Fake) backup.ObjectStore { return lossyStore{f} }, month: jan, wantErr: backup.ErrObjectMissing},
		{name: "dr digest differs", dr: func(f *backupfake.Fake) backup.ObjectStore { return corruptStore{f} }, month: jan, wantErr: backup.ErrObjectMissing},
		{name: "invalid month", dr: asStore, month: backup.Month{Year: 2026, Month: 13}, wantErr: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prim := backupfake.New("primary")
			drFake := backupfake.New("dr")
			if tc.failPrim {
				prim.FailPut = errBoom
			}
			if tc.failDRPut {
				drFake.FailPut = errBoom
			}
			dr := tc.dr(drFake)
			recs := newRecords(dr)
			ex := backup.NewExporter(prim, dr, recs)

			// rule: ARC-U-042
			rec, err := ex.ExportMonth(ctx, tc.month, strings.NewReader("month-snapshot"))
			if !tc.wantRow {
				if err == nil {
					t.Fatalf("ExportMonth: want error, got record %+v", rec)
				}
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("ExportMonth error = %v, want %v", err, tc.wantErr)
				}
				if recs.inserted != 0 {
					t.Fatalf("record inserted without a confirmed DR object")
				}
				return
			}
			if err != nil {
				t.Fatalf("ExportMonth: %v", err)
			}
			if !recs.sawInDR {
				t.Fatalf("record inserted before object was in the DR bucket")
			}
			if rec.Object.Key != backup.ExportKey(tc.month) || rec.Object.Size != int64(len("month-snapshot")) {
				t.Fatalf("record object = %+v", rec.Object)
			}
			got, err := ex.Export(ctx, tc.month)
			if err != nil || got != rec {
				t.Fatalf("Export = %+v, %v; want %+v", got, err, rec)
			}
		})
	}
}

func TestARC_U_042_ExportMonthOnce(t *testing.T) {
	ctx := context.Background()
	m := backup.Month{Year: 2026, Month: time.March}
	prim, dr := backupfake.New("primary"), backupfake.New("dr")
	ex := backup.NewExporter(prim, dr, newRecords(dr))
	if _, err := ex.ExportMonth(ctx, m, strings.NewReader("a")); err != nil {
		t.Fatalf("first export: %v", err)
	}
	tests := []struct {
		name    string
		call    func() error
		wantErr error
	}{
		{name: "second export", call: func() error { _, err := ex.ExportMonth(ctx, m, strings.NewReader("b")); return err }, wantErr: backup.ErrAlreadyExported},
		{name: "unknown month", call: func() error { _, err := ex.Export(ctx, backup.Month{Year: 2026, Month: time.April}); return err }, wantErr: backup.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSLO_05b_ReplicationLagAlertRenders(t *testing.T) {
	tests := []struct {
		name    string
		cell    string
		wantErr bool
	}{
		{name: "cell", cell: "cell-a"},
		{name: "empty cell", cell: "", wantErr: true},
		{name: "injection", cell: "a\"} or vector(1)", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := backup.ReplicationLagAlert(tc.cell)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReplicationLagAlert: %v", err)
			}
			var doc struct {
				Groups []struct {
					Rules []struct {
						Alert  string            `yaml:"alert"`
						Expr   string            `yaml:"expr"`
						Labels map[string]string `yaml:"labels"`
					} `yaml:"rules"`
				} `yaml:"groups"`
			}
			if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("alert is not YAML: %v\n%s", err, out)
			}
			r := doc.Groups[0].Rules[0]
			wantExpr := fmt.Sprintf("max(%s{cell=\"%s\"}) >= 900", backup.ReplicationLagMetric, tc.cell)
			if r.Alert != "DocReplicationLag" || r.Expr != wantExpr || r.Labels["severity"] != "page" || r.Labels["cell"] != tc.cell {
				t.Fatalf("rule = %+v", r)
			}
		})
	}
}

var errBoom = errors.New("boom")

func asStore(f *backupfake.Fake) backup.ObjectStore { return f }
