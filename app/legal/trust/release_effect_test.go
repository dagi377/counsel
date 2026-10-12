package trust_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// memReleases is an in-memory trust.ReleaseStore over one report.
type memReleases struct {
	r        trust.Report
	releases int
}

func (m *memReleases) LockReport(_ context.Context, _ pgx.Tx, id string) (trust.Report, error) {
	if id != m.r.ID {
		return trust.Report{}, trust.ErrNotFound
	}
	return m.r, nil
}

func (m *memReleases) Release(_ context.Context, _ pgx.Tx, _, recipient, _, gateID string, _ time.Time) error {
	m.r.Status, m.r.Recipient, m.r.ApprovalID = "released", recipient, gateID
	m.releases++
	return nil
}

func TestReleaseEffect(t *testing.T) {
	const report = "0192a000-0000-7000-8000-0000000000a9"
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	payload, _ := json.Marshal(map[string]string{"report_id": report, "recipient": "Law Society of Ontario"})
	tests := []struct {
		name      string
		status    string
		approvers []registry.Principal
		repeat    bool
		want      int
		err       error
	}{
		{"ready report released and audited", "ready", []registry.Principal{user("lawyer")}, false, 1, nil},
		// rule: ARC-I-012
		{"repeat is a no-op", "ready", []registry.Principal{user("lawyer")}, true, 1, nil},
		{"released under another gate refused", "released", []registry.Principal{user("lawyer")}, false, 0, trust.ErrReleased},
		// rule: ARC-I-021
		{"report preparer refused", "ready", []registry.Principal{user("bookkeeper")}, false, 0, decider.ErrPreparerApproves},
		// rule: ARC-I-019
		{"agent approver refused", "ready", []registry.Principal{{Kind: registry.PrincipalAgent}}, false, 0, decider.ErrAgentApproves},
		// rule: ARC-I-027
		{"gate preparer refused", "ready", []registry.Principal{user("clerk")}, false, 0, decider.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memReleases{r: trust.Report{ID: report, Kind: "lso_annual_report", PreparedBy: "bookkeeper", Status: tt.status}}
			log := auditfake.New("approval_id", "kind", "recipient")
			e := &trust.ReleaseEffect{Store: st, Audit: log, Now: func() time.Time { return time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC) }}
			g := registry.Approved{GateID: "gate-1", Kind: trust.GateRegulatoryRelease, Payload: payload, Preparer: user("clerk"), Approvers: tt.approvers}
			err := e.Apply(context.Background(), nil, g)
			if tt.repeat && err == nil {
				err = e.Apply(context.Background(), nil, g)
			}
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			// rule: BIL-L-041
			if st.releases != tt.want || len(log.Events()) != tt.want {
				t.Fatalf("releases = %d, audit events = %d, want %d", st.releases, len(log.Events()), tt.want)
			}
		})
	}
}
