package reportfake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/report"
	"github.com/jackc/pgx/v5"
)

var t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func rowsFn(context.Context, pgx.Tx, json.RawMessage) (report.Rows, error) {
	return report.Rows{Columns: []string{"item", "cents"}, Rows: [][]string{{"a", "100"}, {"b, c", "200"}}}, nil
}

func newFake(t *testing.T) *Fake {
	t.Helper()
	f := New(func() time.Time { return t0 }, Caps{"ann": {"ops.view"}})
	if err := f.Register(report.ReportDef{ID: "ops.aging", Capability: "ops.view", Run: rowsFn}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name string
		def  report.ReportDef
		want error
	}{
		{name: "valid", def: report.ReportDef{ID: "ops.wip", Capability: "ops.view", Run: rowsFn}},
		{name: "duplicate", def: report.ReportDef{ID: "ops.aging", Capability: "ops.view", Run: rowsFn}, want: report.ErrDuplicate},
		{name: "no module prefix", def: report.ReportDef{ID: "aging", Capability: "ops.view", Run: rowsFn}, want: report.ErrInvalid},
		{name: "no capability", def: report.ReportDef{ID: "ops.x", Run: rowsFn}, want: report.ErrInvalid},
		{name: "no run func", def: report.ReportDef{ID: "ops.y", Capability: "ops.view"}, want: report.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := newFake(t).Register(tt.def); !errors.Is(err, tt.want) {
				t.Fatalf("Register err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		report string
		params string
		by     string
		want   error
	}{
		{name: "runs", report: "ops.aging", params: `{"as_of":"2026-09-30"}`, by: "ann"},
		{name: "missing capability", report: "ops.aging", params: `{}`, by: "bob", want: report.ErrForbidden},
		{name: "unknown report", report: "ops.none", params: `{}`, by: "ann", want: report.ErrNotFound},
		{name: "params not an object", report: "ops.aging", params: `[]`, by: "ann", want: report.ErrInvalidParams},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			r, rows, err := f.Run(ctx, nil, tt.report, json.RawMessage(tt.params), tt.by)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Run err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			want, _ := ParamsHash(json.RawMessage(tt.params))
			if r.RowCount != 2 || len(rows.Rows) != 2 || r.ParamsHash != want || !strings.HasPrefix(want, "sha256:") {
				t.Fatalf("run %+v", r)
			}
			runs, _ := f.Runs(ctx, tt.report)
			if len(runs) != 1 || runs[0].ID != r.ID {
				t.Fatalf("runs %+v", runs)
			}
		})
	}
}

func TestRunSlot(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name        string
		at          []time.Time
		wantCreated []bool
		wantRuns    int
	}{
		{name: "one slot once", at: []time.Time{t0.Add(time.Hour), t0.Add(2 * time.Hour)}, wantCreated: []bool{true, false}, wantRuns: 1},
		{name: "next slot runs", at: []time.Time{t0, t0.Add(24 * time.Hour)}, wantCreated: []bool{true, true}, wantRuns: 2},
		{name: "rerun same slot three times", at: []time.Time{t0, t0, t0}, wantCreated: []bool{true, false, false}, wantRuns: 1},
		{name: "slot before anchor", at: []time.Time{t0.Add(-time.Hour), t0.Add(-23 * time.Hour)}, wantCreated: []bool{true, false}, wantRuns: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t)
			if err := f.AddSchedule(ctx, nil, report.Schedule{ID: "s1", ReportID: "ops.aging", Params: json.RawMessage(`{}`), Every: 24 * time.Hour, Anchor: t0, Owner: "ann"}); err != nil {
				t.Fatal(err)
			}
			for i, at := range tt.at {
				r, created, err := f.RunSlot(ctx, nil, "s1", at)
				if err != nil || created != tt.wantCreated[i] || r.ScheduleID != "s1" {
					t.Fatalf("RunSlot #%d = %+v, %v, %v", i, r, created, err)
				}
			}
			if runs, _ := f.Runs(ctx, "ops.aging"); len(runs) != tt.wantRuns {
				t.Fatalf("runs = %d, want %d", len(runs), tt.wantRuns)
			}
		})
	}
	f := newFake(t)
	if _, _, err := f.RunSlot(ctx, nil, "nope", t0); !errors.Is(err, report.ErrNotFound) {
		t.Fatalf("unknown schedule err = %v", err)
	}
	if err := f.AddSchedule(ctx, nil, report.Schedule{ID: "s2", ReportID: "ops.none", Params: json.RawMessage(`{}`), Every: time.Hour, Owner: "ann"}); !errors.Is(err, report.ErrNotFound) {
		t.Fatalf("schedule of unknown report err = %v", err)
	}
}

func TestExport(t *testing.T) {
	ctx := context.Background()
	f := newFake(t)
	run, rows, err := f.Run(ctx, nil, "ops.aging", json.RawMessage(`{}`), "ann")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		format     report.Format
		want       error
		wantPrefix string
	}{
		{name: "csv", format: report.FormatCSV, wantPrefix: "item,cents\na,100\n\"b, c\",200\n"},
		{name: "pdf", format: report.FormatPDF, wantPrefix: "%PDF-"},
		{name: "unknown", format: "xlsx", want: report.ErrFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := f.Export(ctx, run, rows, tt.format)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Export err = %v, want %v", err, tt.want)
			}
			if err != nil {
				return
			}
			sum := sha256.Sum256(e.Content)
			m := e.Manifest
			if !strings.HasPrefix(string(e.Content), tt.wantPrefix) || m.SHA256 != hex.EncodeToString(sum[:]) ||
				m.Bytes != len(e.Content) || m.RowCount != 2 || m.RunID != run.ID || m.Format != tt.format {
				t.Fatalf("export %q manifest %+v", e.Content, m)
			}
		})
	}
}
