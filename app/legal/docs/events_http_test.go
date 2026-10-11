package docs

import (
	"context"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
)

type revisionsStub struct {
	matter string
	calls  int
}

func (s *revisionsStub) MatterOf(context.Context, string) (string, error) { return s.matter, nil }

func (s *revisionsStub) Request(context.Context, string, string, string) error {
	s.calls++
	return nil
}

type courtFormsStub struct{ calls int }

func (s *courtFormsStub) Record(context.Context, CourtFormVersion) (bool, error) {
	s.calls++
	return true, nil
}

// rule: ARC-I-042
// rule: ARC-I-051
func TestDocEventRoutesAccess(t *testing.T) {
	const form = `{"rule_set":"RCP","form_number":"14A","title":"Statement of claim","source_url":"https://example.test/14a","version_date":"2026-09-01","checksum":"ab12"}`
	tests := []struct {
		name   string
		user   string
		matter string
		path   string
		body   string
		status int
		writes int
	}{
		{"revision: walled matter is not found", eventroutetest.Walled, "m1", "/documents/d1/revision-requests", `{"version_id":"v1"}`, 404, 0},
		{"revision: no capability on the matter", eventroutetest.NoCap, "m1", "/documents/d1/revision-requests", `{"version_id":"v1"}`, 403, 0},
		{"revision: allowed user", eventroutetest.Lawyer, "m1", "/documents/d1/revision-requests", `{"version_id":"v1"}`, 200, 1},
		{"revision: firm document checks firm-wide only", eventroutetest.NoCap, "", "/documents/d1/revision-requests", `{"version_id":"v1"}`, 200, 1},
		{"revision: no capability firm-wide", eventroutetest.NoFirmCap, "m1", "/documents/d1/revision-requests", `{"version_id":"v1"}`, 403, 0},
		{"court form: no capability", eventroutetest.NoFirmCap, "", "/court-forms", form, 403, 0},
		{"court form: allowed user", eventroutetest.Lawyer, "", "/court-forms", form, 201, 1},
		{"court form: bad checksum", eventroutetest.Lawyer, "", "/court-forms", `{"rule_set":"RCP","form_number":"14A","title":"t","source_url":"u","version_date":"2026-09-01","checksum":"zz"}`, 400, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rev, forms := &revisionsStub{matter: tt.matter}, &courtFormsStub{}
			h := &EventHTTP{Route: eventroutetest.Route(eventroutetest.User(tt.user)), Revisions: rev, CourtForms: forms,
				Now: func() time.Time { return time.Unix(0, 0) }}
			rec := eventroutetest.Post(h.Routes, tt.path, tt.body)
			if rec.Code != tt.status || rev.calls+forms.calls != tt.writes {
				t.Fatalf("status %d writes %d, want %d and %d: %s", rec.Code, rev.calls+forms.calls, tt.status, tt.writes, rec.Body)
			}
		})
	}
}
