package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// SubjectRegulatoryReport is the gate subject kind of a trust.regulatory_report row.
const SubjectRegulatoryReport = "trust.regulatory_report"

// ReportKindAnnual is the LSO annual report kind of trust.regulatory_report.
const ReportKindAnnual = "lso_annual_report"

// ReportDraft is a regulatory report as its preparer records it.
type ReportDraft struct {
	Kind       string
	Year       int
	DocumentID string // optional
	PreparedBy string
}

// ReportPreparer records a prepared regulatory report. PGReleases implements it.
type ReportPreparer interface {
	// Prepare records d as a ready report and returns its id.
	Prepare(ctx context.Context, tx pgx.Tx, d ReportDraft) (string, error)
}

// ReportHTTP serves POST /trust/regulatory-reports: it records an LSO regulatory report draft
// and opens trust.regulatory_release with the acting user as preparer. Only the gate's
// effect releases the report, to the approved recipient.
type ReportHTTP struct {
	Route   eventroute.Route
	DB      eventroute.TxRunner
	Gates   eventroute.Gates
	Reports ReportPreparer
	// MinApprovers is the trust.regulatory_release approver count for the Gate body.
	MinApprovers int
}

func init() { add(registerReportHTTP) }

// registerReportHTTP mounts POST /trust/regulatory-reports. Boot wiring supplies the dependencies; until it does, the
// route answers 503.
func registerReportHTTP(r registry.Registry) error {
	r.Routes((&ReportHTTP{}).Routes)
	return nil
}

// Routes mounts the route.
func (h *ReportHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The route records the draft; the release is trust.regulatory_release's.
	r.Method(http.MethodPost, "/trust/regulatory-reports", server.Declare(h.create))
}

type reportBody struct {
	Kind       string `json:"kind"`
	Year       int    `json:"year"`
	DocumentID string `json:"document_id"`
	Recipient  string `json:"recipient"`
}

func (b reportBody) valid() bool {
	return (b.Kind == "" || b.Kind == ReportKindAnnual || b.Kind == "regulatory_release") &&
		b.Year >= 2000 && b.Year <= 2100 && (b.DocumentID == "" || idPattern.MatchString(b.DocumentID)) &&
		strings.TrimSpace(b.Recipient) != "" && len(b.Recipient) <= 200
}

func (h *ReportHTTP) create(w http.ResponseWriter, r *http.Request) {
	ready := h.DB != nil && h.Gates != nil && h.Reports != nil
	h.Route.Serve(w, r, ready, CapRecord, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b reportBody
		if eventroute.Decode(r, &b) != nil || !b.valid() {
			h.Route.Invalid(w, r, "Body is not valid")
			return
		}
		if b.Kind == "" {
			b.Kind = ReportKindAnnual
		}
		h.Route.OpenGate(w, r, h.DB, h.Gates, p, GateRegulatoryRelease, h.MinApprovers,
			func(ctx context.Context, tx pgx.Tx) (eventroute.Prepared, error) {
				// rule: ARC-I-027
				// The acting user is recorded as the preparer; the effect refuses them as releaser.
				id, err := h.Reports.Prepare(ctx, tx, ReportDraft{Kind: b.Kind, Year: b.Year, DocumentID: b.DocumentID,
					PreparedBy: eventroute.User(p)})
				if errors.Is(err, ErrInvalid) {
					return eventroute.Prepared{}, &eventroute.Refusal{Status: http.StatusUnprocessableEntity,
						Code: server.CodeValidationFailed, Title: "Report is not valid"}
				}
				if err != nil {
					return eventroute.Prepared{}, err
				}
				raw, err := json.Marshal(releasePayload{ReportID: id, Recipient: strings.TrimSpace(b.Recipient)})
				if err != nil {
					return eventroute.Prepared{}, err
				}
				return eventroute.Prepared{Subject: gate.Subject{Kind: SubjectRegulatoryReport, ID: id}, Payload: raw}, nil
			})
	})
}

var _ ReportPreparer = PGReleases{}

const prepareReportSQL = `INSERT INTO trust.regulatory_report (kind, year, document_id, prepared_by)
VALUES ($1, $2, NULLIF($3, '')::uuid, $4::uuid) RETURNING id::text`

// Prepare implements ReportPreparer.
func (PGReleases) Prepare(ctx context.Context, tx pgx.Tx, d ReportDraft) (string, error) {
	if d.Kind == "" || d.PreparedBy == "" {
		return "", fmt.Errorf("prepare report: %w", ErrInvalid)
	}
	var id string
	if err := tx.QueryRow(ctx, prepareReportSQL, d.Kind, d.Year, d.DocumentID, d.PreparedBy).Scan(&id); err != nil {
		return "", fmt.Errorf("prepare report: %w", err)
	}
	return id, nil
}
