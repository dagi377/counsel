package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// Gate kinds whose effects this package supplies (GatePortalMessage, GateSummaryAccuracy and
// GateResearchMemo are declared with their features).
const (
	GateOutgoingEmail     = "platform.outgoing_email"
	GateFileDestruction   = "platform.file_destruction"
	GateIntegrationScope  = "platform.integration_scope"
	GateExport            = "platform.export"
	GateSecurityAnswer    = "platform.security_answer"
	GateOutboundConnector = "platform.outbound_connector"
	GatePaymentProvider   = "platform.payment_provider"
	GateImportBalances    = "platform.import_balances"
	GateVendorSupport     = "platform.vendor_support"
	GateEscrowDestination = "platform.escrow_destination"
	GateServiceCredit     = "platform.service_credit"
	GatePublishedPrice    = "platform.published_price"
)

const (
	// One release row per approving gate; a re-run inserts nothing.
	sqlRelease = `INSERT INTO platform.release (gate_id, kind, matter_id, ref_id, recipient, approved_by, approved_at)
VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5, $6::uuid, $7)
ON CONFLICT (gate_id) DO NOTHING`
	// The latest approved summary replaces the matter's; the same gate again changes nothing.
	sqlSummary = `INSERT INTO platform.matter_summary (matter_id, body_markdown, gate_id, confirmed_by, confirmed_at)
VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5)
ON CONFLICT (matter_id) DO UPDATE SET body_markdown = EXCLUDED.body_markdown, gate_id = EXCLUDED.gate_id,
  confirmed_by = EXCLUDED.confirmed_by, confirmed_at = EXCLUDED.confirmed_at
WHERE platform.matter_summary.gate_id <> EXCLUDED.gate_id`
	sqlDestruction = `INSERT INTO platform.destruction_order (gate_id, matter_id, retention_class, approved_by, approved_at)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5)
ON CONFLICT (gate_id) DO NOTHING`
	// The connector takes the approved scopes only when its data location is what the approver
	// was shown; a pending or active connector moves to active under this gate.
	sqlIntegrationScope = `UPDATE platform.connector SET scopes = $2::text[], status = 'active', approval_id = $3::uuid
WHERE id = $1::uuid AND status IN ('pending','active') AND (data_location <> 'canada') = $4`
	sqlOutboundConnector = `UPDATE platform.connector
SET settings = settings || jsonb_build_object('destination', $2::text), status = 'active', approval_id = $3::uuid
WHERE id = $1::uuid AND direction IN ('out','both') AND status IN ('pending','active') AND (data_location <> 'canada') = $4`
	sqlPaymentProvider = `UPDATE platform.connector SET status = 'active', approval_id = $3::uuid
WHERE kind = 'payments' AND provider = $1 AND credential_ref = $2 AND status IN ('pending','active')`
	// The approved export job is created under the gate's id; a re-run inserts nothing.
	sqlExport = `INSERT INTO platform.export_job (id, scope, requested_by, approval_id, status)
VALUES ($1::uuid, $2::jsonb, $3::uuid, $1::uuid, 'approved')
ON CONFLICT (id) DO NOTHING`
	// The import in review whose tie-out total is the approved total commits under this gate.
	sqlImportBalances = `UPDATE platform.import_job SET status = 'committed', approval_id = $2::uuid
WHERE id = $1::uuid
  AND (status = 'review' OR (status = 'committed' AND approval_id = $2::uuid))
  AND trust_evidence->>'trust_account_id' = $3
  AND (trust_evidence->>'total_cents')::bigint = $4`
	sqlVendorSupport = `INSERT INTO platform.support_access (gate_id, support_user, scope, expires_at, approved_by, approved_at)
VALUES ($1::uuid, $2, $3, $4, $5::uuid, $6)
ON CONFLICT (gate_id) DO NOTHING`
	sqlEscrowDestination = `INSERT INTO platform.escrow_destination (singleton, destination, gate_id, approved_by, approved_at)
VALUES (true, $1, $2::uuid, $3::uuid, $4)
ON CONFLICT (singleton) DO UPDATE SET destination = EXCLUDED.destination, gate_id = EXCLUDED.gate_id,
  approved_by = EXCLUDED.approved_by, approved_at = EXCLUDED.approved_at
WHERE platform.escrow_destination.gate_id <> EXCLUDED.gate_id`
	// One service-credit readiness record per gate.
	sqlServiceCredit = `INSERT INTO platform.readiness_record (kind, name, status, data, approved_by, at)
SELECT 'service_credit', 'service credit', 'approved',
  jsonb_build_object('gate_id', $1::text, 'credit_cents', $2::bigint, 'reason', $3::text), $4::uuid, $5
WHERE NOT EXISTS (SELECT 1 FROM platform.readiness_record
  WHERE kind = 'service_credit' AND data->>'gate_id' = $1::text)`
	sqlPublishedPrice = `INSERT INTO platform.published_price (plan, price_cents, gate_id, approved_by, approved_at)
VALUES ($1, $2, $3::uuid, $4::uuid, $5)
ON CONFLICT (plan) DO UPDATE SET price_cents = EXCLUDED.price_cents, gate_id = EXCLUDED.gate_id,
  approved_by = EXCLUDED.approved_by, approved_at = EXCLUDED.approved_at
WHERE platform.published_price.gate_id <> EXCLUDED.gate_id`
)

// apply is one effect body over the narrow tx surface, so tests run without a database.
type apply func(ctx context.Context, tx gates.Execer, g registry.Approved, by string, now time.Time) error

// effect wraps body: it refuses an agent approver and the preparer, then runs body at now().
func effect(now func() time.Time, body apply) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return runEffect(ctx, tx, g, now, body)
	}
}

func runEffect(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time, body apply) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	return body(ctx, tx, g, by, now())
}

// execKeyed runs a statement keyed by the gate (insert or replace-if-other-gate): no row
// affected means the same gate already applied it, which is a no-op, not an error.
func execKeyed(ctx context.Context, tx gates.Execer, g registry.Approved, sql string, args ...any) error {
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return nil
}

func notApprovable(g registry.Approved, what string) error {
	return fmt.Errorf("approve %s (gate %s): %s: %w", g.Kind, g.GateID, what, gates.ErrNotApprovable)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type releasePayload struct {
	MatterID          string   `json:"matter_id"`
	BodyMarkdown      string   `json:"body_markdown"`
	ToContactIDs      []string `json:"to_contact_ids"`
	Subject           string   `json:"subject"`
	DocumentVersionID string   `json:"document_version_id"`
	QuestionnaireID   string   `json:"questionnaire_id"`
	Recipient         string   `json:"recipient"`
}

// queue inserts the release row for an approved outbound item; the content stays in the gate
// payload, which the sender reads by gate id.
func queue(ctx context.Context, tx gates.Execer, g registry.Approved, kind, matterID, refID, recipient, by string, at time.Time) error {
	// rule: ARC-I-012
	// An outbound item leaves the firm only through its approving gate.
	return execKeyed(ctx, tx, g, sqlRelease, g.GateID, kind, nilIfEmpty(matterID), nilIfEmpty(refID), nilIfEmpty(recipient), by, at)
}

// PortalMessageEffect (G-PM): the approved message is queued for the matter's portal.
func PortalMessageEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, portalMessage)
}

func portalMessage(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p releasePayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.BodyMarkdown == "" {
		return notApprovable(g, "no matter or body")
	}
	return queue(ctx, tx, g, "portal_message", p.MatterID, "", "", by, at)
}

// OutgoingEmailEffect (G-OUT): the approved email is queued for sending to its contacts.
func OutgoingEmailEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, outgoingEmail)
}

func outgoingEmail(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p releasePayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || len(p.ToContactIDs) == 0 || p.Subject == "" || p.BodyMarkdown == "" {
		return notApprovable(g, "incomplete email")
	}
	return queue(ctx, tx, g, "outgoing_email", p.MatterID, "", "", by, at)
}

// ResearchMemoEffect (G-RES): the approved memo version is released on its matter.
func ResearchMemoEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, researchMemo)
}

func researchMemo(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p releasePayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.DocumentVersionID == "" {
		return notApprovable(g, "no memo version")
	}
	return queue(ctx, tx, g, "research_memo", p.MatterID, p.DocumentVersionID, "", by, at)
}

// SecurityAnswerEffect (G-SEC): the approved questionnaire answers are queued for the recipient.
func SecurityAnswerEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, securityAnswer)
}

func securityAnswer(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p releasePayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.QuestionnaireID == "" || p.Recipient == "" {
		return notApprovable(g, "no questionnaire or recipient")
	}
	return queue(ctx, tx, g, "security_answer", "", p.QuestionnaireID, p.Recipient, by, at)
}

// SummaryAccuracyEffect (G-SUM): the approved summary becomes the matter's confirmed summary.
func SummaryAccuracyEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, summaryAccuracy)
}

func summaryAccuracy(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p releasePayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.BodyMarkdown == "" {
		return notApprovable(g, "no summary")
	}
	return execKeyed(ctx, tx, g, sqlSummary, p.MatterID, p.BodyMarkdown, g.GateID, by, at)
}

// FileDestructionEffect (G14): the matter's files are ordered destroyed under the approved
// retention class; the retention job executes the order and re-checks legal hold.
func FileDestructionEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, fileDestruction)
}

func fileDestruction(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p struct {
		MatterID       string `json:"matter_id"`
		RetentionClass string `json:"retention_class"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.RetentionClass == "" {
		return notApprovable(g, "no matter or retention class")
	}
	// rule: ARC-I-012
	return execKeyed(ctx, tx, g, sqlDestruction, g.GateID, p.MatterID, p.RetentionClass, by, at)
}

type connectorPayload struct {
	IntegrationID    string   `json:"integration_id"`
	Scopes           []string `json:"scopes"`
	Destination      string   `json:"destination"`
	DataLeavesCanada *bool    `json:"data_leaves_canada"`
}

// IntegrationScopeEffect (G-INT): the connector is activated with exactly the approved scopes.
func IntegrationScopeEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, integrationScope)
}

func integrationScope(ctx context.Context, tx gates.Execer, g registry.Approved, _ string, _ time.Time) error {
	var p connectorPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.IntegrationID == "" || len(p.Scopes) == 0 || p.DataLeavesCanada == nil {
		return notApprovable(g, "incomplete integration scope")
	}
	// rule: ARC-U-002
	// The approver saw whether data leaves Canada; a connector that says otherwise does not move.
	return gates.ExecOne(ctx, tx, g, sqlIntegrationScope, p.IntegrationID, p.Scopes, g.GateID, *p.DataLeavesCanada)
}

// OutboundConnectorEffect (G-INT readiness): the outbound connector is activated to the
// approved destination.
func OutboundConnectorEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, outboundConnector)
}

func outboundConnector(ctx context.Context, tx gates.Execer, g registry.Approved, _ string, _ time.Time) error {
	var p connectorPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.IntegrationID == "" || p.Destination == "" || p.DataLeavesCanada == nil {
		return notApprovable(g, "incomplete outbound connector")
	}
	// rule: ARC-U-002
	return gates.ExecOne(ctx, tx, g, sqlOutboundConnector, p.IntegrationID, p.Destination, g.GateID, *p.DataLeavesCanada)
}

// PaymentProviderEffect (G-PAY): the payments connector for the approved provider and account
// reference is activated.
func PaymentProviderEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, paymentProvider)
}

func paymentProvider(ctx context.Context, tx gates.Execer, g registry.Approved, _ string, _ time.Time) error {
	var p struct {
		Provider   string `json:"provider"`
		AccountRef string `json:"account_ref"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.Provider == "" || p.AccountRef == "" {
		return notApprovable(g, "no provider or account")
	}
	// rule: ARC-I-012
	return gates.ExecOne(ctx, tx, g, sqlPaymentProvider, p.Provider, p.AccountRef, g.GateID)
}

// ExportEffect (G-EXP): an export job for the approved scope and recipient is created,
// approved, under the gate's id.
func ExportEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, export)
}

func export(ctx context.Context, tx gates.Execer, g registry.Approved, _ string, _ time.Time) error {
	var p struct {
		ReportID  string `json:"report_id,omitempty"`
		Scope     string `json:"scope"`
		Recipient string `json:"recipient"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.Scope == "" || p.Recipient == "" {
		return notApprovable(g, "no scope or recipient")
	}
	requester := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		requester = g.Preparer.ActingFor
	}
	if requester == "" {
		return notApprovable(g, "no requester")
	}
	scope, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("approve %s (gate %s): scope: %w", g.Kind, g.GateID, err)
	}
	// rule: ARC-I-012
	return execKeyed(ctx, tx, g, sqlExport, g.GateID, string(scope), requester)
}

// ImportBalancesEffect (G-IMP): the import in review whose tie-out matches the approved trust
// account and total commits; its opening balance lines may then post (MIG-033).
func ImportBalancesEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, importBalances)
}

func importBalances(ctx context.Context, tx gates.Execer, g registry.Approved, _ string, _ time.Time) error {
	var p struct {
		ImportID       string `json:"import_id"`
		TrustAccountID string `json:"trust_account_id"`
		TotalCents     *int64 `json:"total_cents"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.ImportID == "" || p.TrustAccountID == "" || p.TotalCents == nil || *p.TotalCents < 0 {
		return notApprovable(g, "incomplete import sign-off")
	}
	// rule: MIG-033
	// Only the exact approved total for the approved account signs the import off.
	return gates.ExecOne(ctx, tx, g, sqlImportBalances, p.ImportID, g.GateID, p.TrustAccountID, *p.TotalCents)
}

// VendorSupportEffect (G-SUP): vendor support gets the approved scope until expires_at.
func VendorSupportEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, vendorSupport)
}

func vendorSupport(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p struct {
		SupportUser string    `json:"support_user"`
		Scope       string    `json:"scope"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.SupportUser == "" || p.Scope == "" {
		return notApprovable(g, "no support user or scope")
	}
	// rule: ARC-I-012
	// Access that would already be expired is not granted.
	if !p.ExpiresAt.After(at) {
		return notApprovable(g, "expired")
	}
	return execKeyed(ctx, tx, g, sqlVendorSupport, g.GateID, p.SupportUser, p.Scope, p.ExpiresAt, by, at)
}

// EscrowDestinationEffect (G-ESC): the approved destination becomes the firm's escrow destination.
func EscrowDestinationEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, escrowDestination)
}

func escrowDestination(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p struct {
		Destination string `json:"destination"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.Destination == "" {
		return notApprovable(g, "no destination")
	}
	// rule: ARC-I-012
	return execKeyed(ctx, tx, g, sqlEscrowDestination, p.Destination, g.GateID, by, at)
}

// ServiceCreditEffect (G-SLA): the approved credit, in cents, is recorded as a readiness
// service-credit record.
func ServiceCreditEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, serviceCredit)
}

func serviceCredit(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p struct {
		AmountCents *int64 `json:"amount_cents"`
		Reason      string `json:"reason"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.AmountCents == nil || *p.AmountCents < 0 || p.Reason == "" {
		return notApprovable(g, "no amount or reason")
	}
	// rule: ARC-I-012
	return execKeyed(ctx, tx, g, sqlServiceCredit, g.GateID, *p.AmountCents, p.Reason, by, at)
}

// PublishedPriceEffect (G-PRICE): the plan's published price becomes the approved cents.
func PublishedPriceEffect(now func() time.Time) registry.EffectFunc {
	return effect(now, publishedPrice)
}

func publishedPrice(ctx context.Context, tx gates.Execer, g registry.Approved, by string, at time.Time) error {
	var p struct {
		Plan       string `json:"plan"`
		PriceCents *int64 `json:"price_cents"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.Plan == "" || p.PriceCents == nil || *p.PriceCents < 0 {
		return notApprovable(g, "no plan or price")
	}
	// rule: ARC-I-012
	return execKeyed(ctx, tx, g, sqlPublishedPrice, p.Plan, *p.PriceCents, g.GateID, by, at)
}
