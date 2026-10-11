package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	kaudit "github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateRegulatoryRelease is the G-TREL gate: approval releases a prepared regulatory report
// (the LSO annual report, or another trust record) to the approved recipient.
const GateRegulatoryRelease = "trust.regulatory_release"

// ActionRegulatoryRelease is the audit action of a release.
const ActionRegulatoryRelease = "trust.regulatory_release"

// ErrReleased means the report was already released under another gate.
var ErrReleased = errors.New("trust: report already released")

// Report is a trust.regulatory_report row (5531), locked in the gate tx.
type Report struct {
	ID         string
	Kind       string
	PreparedBy string
	Status     string // ready, released
	Recipient  string
	ApprovalID string
}

// ReleaseStore keeps regulatory reports. Every method runs in the gate tx. PGReleases
// implements it.
type ReleaseStore interface {
	// LockReport locks report id, or ErrNotFound.
	LockReport(ctx context.Context, tx pgx.Tx, id string) (Report, error)
	// Release marks the ready report released to recipient under gate gateID.
	Release(ctx context.Context, tx pgx.Tx, id, recipient, by, gateID string, at time.Time) error
}

type releasePayload struct {
	ReportID  string `json:"report_id"`
	Recipient string `json:"recipient"`
}

// ReleaseEffect is the trust.regulatory_release EffectFunc: it marks the report released and
// audits the release in the same tx.
type ReleaseEffect struct {
	Store ReleaseStore
	Audit kaudit.Log
	Now   func() time.Time
}

// Apply releases the report. A report this gate already released is a no-op.
func (e *ReleaseEffect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	by, err := decider.Check(g)
	if err != nil {
		return fmt.Errorf("regulatory release: %w", err)
	}
	if e == nil || e.Store == nil || e.Audit == nil || e.Now == nil {
		return fmt.Errorf("regulatory release %s: not wired: %w", g.GateID, ErrInvalid)
	}
	var p releasePayload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.ReportID == "" || strings.TrimSpace(p.Recipient) == "" ||
		(g.Subject.ID != "" && g.Subject.ID != p.ReportID) {
		return fmt.Errorf("regulatory release %s: payload: %w", g.GateID, ErrInvalid)
	}
	r, err := e.Store.LockReport(ctx, tx, p.ReportID)
	if err != nil {
		return fmt.Errorf("regulatory release %s: %w", g.GateID, err)
	}
	// rule: ARC-I-012
	// Idempotent per gate.
	if r.Status == "released" && r.ApprovalID == g.GateID {
		return nil
	}
	if r.Status != "ready" {
		return fmt.Errorf("regulatory release %s: report %s is %s: %w", g.GateID, r.ID, r.Status, ErrReleased)
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-021
		// The person who prepared the report never releases it.
		if a.UserID == r.PreparedBy {
			return fmt.Errorf("regulatory release %s: %w", g.GateID, decider.ErrPreparerApproves)
		}
	}
	at := e.Now()
	if err := e.Store.Release(ctx, tx, r.ID, p.Recipient, by, g.GateID, at); err != nil {
		return fmt.Errorf("regulatory release %s: %w", g.GateID, err)
	}
	// rule: BIL-L-041
	// The release of a trust record to the regulator is audited in the same tx.
	ev := kaudit.Event{At: at, ActorKind: "user", ActorID: by, Action: ActionRegulatoryRelease,
		Subject: kaudit.Subject{Kind: "trust.regulatory_report", ID: r.ID},
		Detail:  map[string]string{"approval_id": g.GateID, "kind": r.Kind, "recipient": p.Recipient}}
	if err := e.Audit.Append(ctx, tx, ev); err != nil {
		return fmt.Errorf("regulatory release %s: audit: %w", g.GateID, err)
	}
	return nil
}

// PGReleases is ReleaseStore over trust.regulatory_report.
type PGReleases struct{}

var _ ReleaseStore = PGReleases{}

const lockReportSQL = `SELECT id::text, kind, prepared_by::text, status, coalesce(recipient, ''), coalesce(approval_id::text, '')
FROM trust.regulatory_report WHERE id = $1::uuid FOR UPDATE`

const releaseReportSQL = `UPDATE trust.regulatory_report SET status = 'released', recipient = $2, released_by = $3::uuid,
  released_at = $5, approval_id = $4::uuid WHERE id = $1::uuid AND status = 'ready'`

// LockReport implements ReleaseStore.
func (PGReleases) LockReport(ctx context.Context, tx pgx.Tx, id string) (Report, error) {
	var r Report
	err := tx.QueryRow(ctx, lockReportSQL, id).Scan(&r.ID, &r.Kind, &r.PreparedBy, &r.Status, &r.Recipient, &r.ApprovalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, fmt.Errorf("regulatory report %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Report{}, fmt.Errorf("regulatory report %s: %w", id, err)
	}
	return r, nil
}

// Release implements ReleaseStore.
func (PGReleases) Release(ctx context.Context, tx pgx.Tx, id, recipient, by, gateID string, at time.Time) error {
	tag, err := tx.Exec(ctx, releaseReportSQL, id, recipient, by, gateID, at)
	if err != nil {
		return fmt.Errorf("release report %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("release report %s: %w", id, ErrReleased)
	}
	return nil
}
