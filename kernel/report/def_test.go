package report_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/report"
	"github.com/dagi377/counsel/kernel/report/reportfake"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

const periodSchema = `{"type":"object","properties":{"period":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}$"}},"required":["period"],"additionalProperties":false}`

func rowsFn(context.Context, pgx.Tx, json.RawMessage) (report.Rows, error) {
	return report.Rows{Columns: []string{"item", "cents"}, Rows: [][]string{{"secret-a", "100"}, {"secret-b", "200"}}}, nil
}

func newEngine(t *testing.T) (*report.Reports, *auditfake.Fake) {
	t.Helper()
	log := auditfake.New("report", "params_hash", "row_count")
	e := report.New(func() time.Time { return t0 }, reportfake.Caps{"ann": {"ops.view"}}, log)
	if err := e.Register(report.ReportDef{ID: "ops.aging", Capability: "ops.view", Params: []byte(periodSchema), Run: rowsFn}); err != nil {
		t.Fatal(err)
	}
	return e, log
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name string
		def  report.ReportDef
		want error
	}{
		{name: "valid", def: report.ReportDef{ID: "ops.wip", Capability: "ops.view", Run: rowsFn}},
		{name: "valid with params schema", def: report.ReportDef{ID: "ops.wip_by_owner", Capability: "ops.view", Params: []byte(periodSchema), Run: rowsFn}},
		{name: "duplicate", def: report.ReportDef{ID: "ops.aging", Capability: "ops.view", Run: rowsFn}, want: report.ErrDuplicate},
		{name: "no module prefix", def: report.ReportDef{ID: "aging", Capability: "ops.view", Run: rowsFn}, want: report.ErrInvalid},
		{name: "no capability", def: report.ReportDef{ID: "ops.x", Run: rowsFn}, want: report.ErrInvalid},
		{name: "no run func", def: report.ReportDef{ID: "ops.y", Capability: "ops.view"}, want: report.ErrInvalid},
		{name: "params not json", def: report.ReportDef{ID: "ops.z", Capability: "ops.view", Params: []byte(`{`), Run: rowsFn}, want: report.ErrInvalid},
		{name: "params not a schema", def: report.ReportDef{ID: "ops.w", Capability: "ops.view", Params: []byte(`{"type":7}`), Run: rowsFn}, want: report.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := newEngine(t)
			err := e.Register(tc.def)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("Register() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name   string
		report string
		params string
		by     string
		want   error
	}{
		{name: "runs", report: "ops.aging", params: `{"period":"2026-09"}`, by: "ann"},
		{name: "unknown report", report: "ops.none", params: `{}`, by: "ann", want: report.ErrNotFound},
		{name: "missing capability", report: "ops.aging", params: `{"period":"2026-09"}`, by: "bob", want: report.ErrForbidden},
		{name: "params not object", report: "ops.aging", params: `[1]`, by: "ann", want: report.ErrInvalidParams},
		{name: "params fail schema", report: "ops.aging", params: `{"period":"Sept"}`, by: "ann", want: report.ErrInvalidParams},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, log := newEngine(t)
			r, rows, err := e.Run(context.Background(), nil, tc.report, json.RawMessage(tc.params), tc.by)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("Run() = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if n := len(log.Events()); n != 0 {
					t.Fatalf("failed run wrote %d audit events", n)
				}
				return
			}
			if r.RowCount != 2 || len(rows.Rows) != 2 || r.By != "ann" || r.ReportID != tc.report {
				t.Fatalf("run = %+v", r)
			}
			runs, err := e.Runs(context.Background(), tc.report)
			if err != nil || len(runs) != 1 || runs[0].ID != r.ID {
				t.Fatalf("Runs() = %v, %v", runs, err)
			}
		})
	}
}

func TestRunAuditHoldsHashAndCountOnly(t *testing.T) {
	e, log := newEngine(t)
	ctx := context.Background()
	a, _, err := e.Run(ctx, nil, "ops.aging", json.RawMessage(`{"period":"2026-09"}`), "ann")
	if err != nil {
		t.Fatal(err)
	}
	// Same params, different key order and spacing: same hash.
	b, _, err := e.Run(ctx, nil, "ops.aging", json.RawMessage(` { "period" : "2026-09" } `), "ann")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := report.ParamsHash(json.RawMessage(`{"period":"2026-09"}`))
	if a.ParamsHash != want || b.ParamsHash != want || !strings.HasPrefix(want, "sha256:") {
		t.Fatalf("hashes %q %q, want %q", a.ParamsHash, b.ParamsHash, want)
	}
	evs := log.Events()
	if len(evs) != 2 {
		t.Fatalf("audit events = %d, want 2", len(evs))
	}
	for _, ev := range evs {
		if ev.Action != report.ActionRun || ev.ActorID != "ann" {
			t.Fatalf("event = %+v", ev)
		}
		wantDetail := map[string]string{"report": "ops.aging", "params_hash": want, "row_count": "2"}
		if len(ev.Detail) != len(wantDetail) {
			t.Fatalf("detail = %v, want %v", ev.Detail, wantDetail)
		}
		for k, v := range wantDetail {
			if ev.Detail[k] != v {
				t.Fatalf("detail[%s] = %q, want %q", k, ev.Detail[k], v)
			}
		}
		for _, v := range ev.Detail {
			if strings.Contains(v, "secret") || strings.Contains(v, "2026-09") {
				t.Fatalf("audit detail leaks row data or params: %v", ev.Detail)
			}
		}
	}
}
