package trust_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
	"github.com/dagi377/counsel/app/legal/trust"
)

type memReports struct{ drafts []trust.ReportDraft }

func (m *memReports) Prepare(_ context.Context, _ pgx.Tx, d trust.ReportDraft) (string, error) {
	m.drafts = append(m.drafts, d)
	return "0192a000-0000-7000-8000-0000000000f1", nil
}

func TestReportHTTPCreate(t *testing.T) {
	tests := []struct {
		name   string
		user   string
		body   string
		status int
		kind   string
	}{
		// rule: ARC-I-027
		{"LSO annual report draft opens the release gate", "bookkeeper", `{"year":2026,"recipient":"Law Society of Ontario"}`, http.StatusAccepted, trust.ReportKindAnnual},
		{"blank recipient refused", "bookkeeper", `{"year":2026,"recipient":" "}`, http.StatusBadRequest, ""},
		{"year out of range refused", "bookkeeper", `{"year":1999,"recipient":"LSO"}`, http.StatusBadRequest, ""},
		{"unknown kind refused", "bookkeeper", `{"kind":"other","year":2026,"recipient":"LSO"}`, http.StatusBadRequest, ""},
		// rule: ARC-I-051
		{"no capability", eventroutetest.NoFirmCap, `{"year":2026,"recipient":"LSO"}`, http.StatusForbidden, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := eventroutetest.User(tt.user)
			gates, reports := &eventroutetest.Gates{}, &memReports{}
			h := &trust.ReportHTTP{Route: eventroutetest.Route(p), DB: eventroutetest.Tx{}, Gates: gates, Reports: reports}
			rec := eventroutetest.Post(h.Routes, "/trust/regulatory-reports", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if tt.kind == "" {
				if len(gates.Opened)+len(reports.drafts) != 0 {
					t.Fatal("a refused request wrote")
				}
				return
			}
			o := gates.Opened[0]
			var pl map[string]string
			_ = json.Unmarshal(o.Payload, &pl)
			if len(reports.drafts) != 1 || reports.drafts[0].PreparedBy != tt.user || reports.drafts[0].Kind != tt.kind ||
				o.Kind != trust.GateRegulatoryRelease || !reflect.DeepEqual(o.Preparer, p) || pl["report_id"] != o.Subject.ID || pl["recipient"] == "" {
				t.Fatalf("drafts %+v opened %+v payload %v", reports.drafts, o, pl)
			}
			var g map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &g)
			if g["state"] != "open" || g["kind"] != trust.GateRegulatoryRelease {
				t.Fatalf("gate body = %v", g)
			}
		})
	}
}
