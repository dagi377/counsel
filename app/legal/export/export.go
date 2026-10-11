package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// Format is the published export format id written into every manifest.
const Format = "counsel-export/1"

// Currency is the only currency an export carries.
const Currency = "CAD"

// FlagPostCancelWindow gates exports for cancelled firms. The 90-day window depends on the
// customer contract, which is unverified, so cancelled firms are refused while it is off.
const FlagPostCancelWindow = legalflags.ExportPostCancelWindow

const (
	// PostCancelWindow is how long after cancellation an owner may still request an export.
	PostCancelWindow = 90 * 24 * time.Hour
	// LinkTTL is how long a delivery link stays valid.
	LinkTTL = 24 * time.Hour
)

var (
	// ErrWindowClosed is returned when a cancelled firm asks for an export after the window.
	ErrWindowClosed = errors.New("export: post-cancellation window closed (customer contract, export clause)")
	// ErrUnverified is returned for a cancelled firm while FlagPostCancelWindow is off.
	ErrUnverified = errors.New("export: post-cancellation window not verified")
	// ErrLinkExpired is returned for a delivery link used after LinkTTL; HTTP maps it to 410.
	ErrLinkExpired = errors.New("export: link expired")
)

// Entities lists, in layout order, every entity written to json/<entity>.ndjson and
// csv/<entity>.csv.
var Entities = []string{
	"users", "contacts", "contact_points", "matters", "matter_parties", "custom_field_defs",
	"custom_field_values", "notes", "communications", "calendar_events", "tasks", "documents",
	"document_versions", "time_entries", "expense_entries", "rate_cards", "bills", "bill_lines",
	"bill_adjustments", "payments", "payment_allocations", "trust_accounts", "signatories",
	"client_trust_ledgers", "trust_transactions", "trust_historical_lines", "withdrawal_requests",
	"trust_transfers", "reconciliations", "reconciling_items", "general_journal_lines",
	"gate_decisions",
}

// CanRequest reports whether an owner may request a full firm export at now. A zero
// cancelledAt means the firm is active.
func CanRequest(ctx context.Context, cancelledAt, now time.Time) error {
	if cancelledAt.IsZero() {
		return nil
	}
	// rule: MIG-070 (verify)
	if !legalflags.Enabled(ctx, FlagPostCancelWindow) {
		return ErrUnverified
	}
	if now.Sub(cancelledAt) > PostCancelWindow {
		return ErrWindowClosed
	}
	return nil
}

// CheckLink reports whether a delivery link issued at issuedAt may still be used at now.
func CheckLink(issuedAt, now time.Time) error {
	// rule: MIG-070
	if now.Sub(issuedAt) > LinkTTL {
		return ErrLinkExpired
	}
	return nil
}

// Root is the top directory name of an export: counsel-export-<firm>-<as_of>.
func Root(firm string, asOf time.Time) string {
	return fmt.Sprintf("counsel-export-%s-%s", firm, asOf.UTC().Format("20060102T150405Z"))
}

// JSONPath is the ndjson file of an entity.
func JSONPath(entity string) string { return "json/" + entity + ".ndjson" }

// CSVPath is the flattened CSV file of an entity.
func CSVPath(entity string) string { return "csv/" + entity + ".csv" }

// DocumentPath is where version n of a document's bytes is written.
func DocumentPath(matterID, documentID string, n int, name string) string {
	return fmt.Sprintf("documents/%s/%s/v%d-%s", matterID, documentID, n, Sanitise(name))
}

// TrustJournalPath is the journal of one trust account.
func TrustJournalPath(accountID string) string { return "ledgers/trust/" + accountID + "/journal.csv" }

// ClientLedgerPath is one client trust ledger of a trust account.
func ClientLedgerPath(accountID, ledgerID string) string {
	return "ledgers/trust/" + accountID + "/client_ledgers/" + ledgerID + ".csv"
}

// ReconciliationPath is a month's reconciliation package; ext is "pdf" or "json".
func ReconciliationPath(accountID string, month time.Time, ext string) string {
	return "ledgers/trust/" + accountID + "/reconciliations/" + month.Format("2006-01") + "." + ext
}

// Fixed layout paths.
const (
	ManifestPath       = "manifest.json"
	SignaturePath      = "manifest.json.sig"
	ReadmePath         = "README.txt"
	GeneralJournalPath = "ledgers/general/journal.csv"
	TrustAuditPath     = "audit/trust_audit_events.ndjson"
	AppAuditPath       = "audit/app_audit_events.ndjson"
)

// LegacyRawPath is an imported raw archive resource of a legacy system region.
func LegacyRawPath(system, region, resource string) string {
	return "legacy/" + system + "/" + region + "/raw/" + resource + ".ndjson"
}

// Sanitise makes a document name safe as one path segment.
func Sanitise(name string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || unicode.IsControl(r):
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	s = strings.TrimLeft(s, ".")
	if s == "" {
		return "_"
	}
	return s
}

// Money is an amount in an export: integer cents plus currency, never a decimal.
type Money struct {
	Cents    int64  `json:"cents"`
	Currency string `json:"currency"`
}

// NewMoney returns cents as export Money in the export currency.
func NewMoney(cents int64) Money {
	// rule: MIG-072
	return Money{Cents: cents, Currency: Currency}
}

// Instant formats an instant as ISO 8601 UTC with a trailing Z.
func Instant(t time.Time) string {
	// rule: MIG-072
	return t.UTC().Format(time.RFC3339Nano)
}

// Date formats a business date as YYYY-MM-DD.
func Date(t time.Time) string { return t.Format(time.DateOnly) }

// CSV writes rows as UTF-8 with a header and RFC 4180 quoting.
func CSV(header []string, rows [][]string) ([]byte, error) {
	// rule: MIG-072
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	if err := w.Write(header); err != nil {
		return nil, fmt.Errorf("export csv header: %w", err)
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, fmt.Errorf("export csv rows: %w", err)
	}
	return b.Bytes(), nil
}
