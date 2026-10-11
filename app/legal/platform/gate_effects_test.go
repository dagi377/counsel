package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// fakeExec records statements and answers rows affected per call (default 1).
type fakeExec struct {
	rows []int64
	sqls []string
	args [][]any
}

func (f *fakeExec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.sqls, f.args = append(f.sqls, sql), append(f.args, args)
	n := int64(1)
	if i := len(f.sqls) - 1; i < len(f.rows) {
		n = f.rows[i]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

var (
	lawyer = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000a"}
	prep   = registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-00000000000b"}
	bot    = registry.Principal{Kind: registry.PrincipalAgent, RunID: "r-1", ActingFor: prep.UserID}
	at     = time.Date(2026, 10, 11, 23, 30, 0, 0, time.UTC)
)

type effectCase struct {
	name     string
	payload  string
	prep, by registry.Principal
	rows     []int64
	wantSQLs int
	wantErr  error
}

// standard is the shared table for one kind: approval applies, a repeat is a no-op, an agent
// or the preparer is refused, and a payload missing what was approved is refused. keyed kinds
// insert by gate (a repeat affects no row); the others match the row in its approved state.
func standard(ok, bad string, keyed bool) []effectCase {
	repeat := []int64{1}
	if keyed {
		repeat = []int64{0}
	}
	return []effectCase{
		// rule: ARC-I-012
		{"approval applies", ok, bot, lawyer, nil, 1, nil},
		{"repeat is a no-op", ok, bot, lawyer, repeat, 1, nil},
		{"payload short of the approval is refused", bad, bot, lawyer, nil, 0, gates.ErrNotApprovable},
		// rule: ARC-I-019
		{"agent never approves", ok, prep, bot, nil, 0, gates.ErrAgentApproves},
		{"preparer never approves", ok, bot, prep, nil, 0, gates.ErrPreparerApproves},
	}
}

func runCases(t *testing.T, kind string, body apply, cases []effectCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeExec{rows: tt.rows}
			g := registry.Approved{GateID: "g-1", Kind: kind, Payload: []byte(tt.payload), Preparer: tt.prep,
				Approvers: []registry.Principal{tt.by}}
			err := run(context.Background(), tx, g, func() time.Time { return at }, body)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(tx.sqls) != tt.wantSQLs {
				t.Fatalf("ran %d statements, want %d", len(tx.sqls), tt.wantSQLs)
			}
		})
	}
}

func TestPortalMessageEffect(t *testing.T) {
	runCases(t, GatePortalMessage, portalMessage,
		standard(`{"matter_id":"m-1","body_markdown":"hi"}`, `{"matter_id":"m-1"}`, true))
}

func TestOutgoingEmailEffect(t *testing.T) {
	runCases(t, GateOutgoingEmail, outgoingEmail, standard(
		`{"matter_id":"m-1","to_contact_ids":["c-1"],"subject":"s","body_markdown":"b"}`,
		`{"matter_id":"m-1","to_contact_ids":[],"subject":"s","body_markdown":"b"}`, true))
}

func TestResearchMemoEffect(t *testing.T) {
	runCases(t, GateResearchMemo, researchMemo,
		standard(`{"matter_id":"m-1","document_version_id":"v-1"}`, `{"matter_id":"m-1"}`, true))
}

func TestSecurityAnswerEffect(t *testing.T) {
	runCases(t, GateSecurityAnswer, securityAnswer,
		standard(`{"questionnaire_id":"q-1","recipient":"r@x"}`, `{"questionnaire_id":"q-1"}`, true))
}

func TestSummaryAccuracyEffect(t *testing.T) {
	runCases(t, GateSummaryAccuracy, summaryAccuracy,
		standard(`{"matter_id":"m-1","body_markdown":"sum"}`, `{"matter_id":"m-1","body_markdown":""}`, true))
}

func TestFileDestructionEffect(t *testing.T) {
	runCases(t, GateFileDestruction, fileDestruction,
		standard(`{"matter_id":"m-1","retention_class":"RC-FILE-15"}`, `{"matter_id":"m-1"}`, true))
}

func TestIntegrationScopeEffect(t *testing.T) {
	ok := `{"integration_id":"i-1","scopes":["mail.read"],"data_leaves_canada":false}`
	cases := append(standard(ok, `{"integration_id":"i-1","scopes":["mail.read"]}`, false),
		// rule: ARC-U-002
		effectCase{"connector not as disclosed does not move", ok, bot, lawyer, []int64{0}, 1, gates.ErrNotApprovable})
	runCases(t, GateIntegrationScope, integrationScope, cases)
}

func TestOutboundConnectorEffect(t *testing.T) {
	ok := `{"integration_id":"i-1","destination":"https://x","data_leaves_canada":true}`
	cases := append(standard(ok, `{"integration_id":"i-1","data_leaves_canada":true}`, false),
		// rule: ARC-U-002
		effectCase{"inbound or other-location connector does not move", ok, bot, lawyer, []int64{0}, 1, gates.ErrNotApprovable})
	runCases(t, GateOutboundConnector, outboundConnector, cases)
}

func TestPaymentProviderEffect(t *testing.T) {
	ok := `{"provider":"cards","account_ref":"vault/pay/1"}`
	cases := append(standard(ok, `{"provider":"cards"}`, false),
		effectCase{"no such payments connector", ok, bot, lawyer, []int64{0}, 1, gates.ErrNotApprovable})
	runCases(t, GatePaymentProvider, paymentProvider, cases)
}

func TestExportEffect(t *testing.T) {
	runCases(t, GateExport, export,
		standard(`{"scope":"firm","recipient":"auditor@x"}`, `{"scope":"firm"}`, true))
}

func TestImportBalancesEffect(t *testing.T) {
	ok := `{"import_id":"imp-1","trust_account_id":"ta-1","total_cents":1234567}`
	cases := append(standard(ok, `{"import_id":"imp-1","trust_account_id":"ta-1"}`, false),
		// rule: MIG-033
		effectCase{"tie-out total differs from the approved total", ok, bot, lawyer, []int64{0}, 1, gates.ErrNotApprovable},
		effectCase{"negative total refused", `{"import_id":"imp-1","trust_account_id":"ta-1","total_cents":-1}`, bot, lawyer, nil, 0, gates.ErrNotApprovable})
	runCases(t, GateImportBalances, importBalances, cases)
}

func TestVendorSupportEffect(t *testing.T) {
	cases := append(standard(`{"support_user":"s-1","scope":"read","expires_at":"2026-10-12T20:00:00Z"}`,
		`{"support_user":"s-1","expires_at":"2026-10-12T20:00:00Z"}`, true),
		// rule: ARC-I-012
		effectCase{"already expired access is not granted",
			`{"support_user":"s-1","scope":"read","expires_at":"2026-10-11T23:00:00Z"}`, bot, lawyer, nil, 0, gates.ErrNotApprovable})
	runCases(t, GateVendorSupport, vendorSupport, cases)
}

func TestEscrowDestinationEffect(t *testing.T) {
	runCases(t, GateEscrowDestination, escrowDestination,
		standard(`{"destination":"escrow-ca-1"}`, `{"destination":""}`, true))
}

func TestServiceCreditEffect(t *testing.T) {
	cases := append(standard(`{"amount_cents":5000,"reason":"downtime"}`, `{"reason":"downtime"}`, true),
		effectCase{"negative credit refused", `{"amount_cents":-1,"reason":"x"}`, bot, lawyer, nil, 0, gates.ErrNotApprovable})
	runCases(t, GateServiceCredit, serviceCredit, cases)
}

func TestPublishedPriceEffect(t *testing.T) {
	cases := append(standard(`{"plan":"solo","price_cents":9900}`, `{"plan":"solo"}`, true),
		effectCase{"negative price refused", `{"plan":"solo","price_cents":-5}`, bot, lawyer, nil, 0, gates.ErrNotApprovable})
	runCases(t, GatePublishedPrice, publishedPrice, cases)
}

// The approved values reach the statement exactly: cents as integers, the gate as the key.
func TestEffectArgs(t *testing.T) {
	tests := []struct {
		name string
		kind string
		body apply
		pay  string
		want []any
	}{
		{"price cents", GatePublishedPrice, publishedPrice, `{"plan":"solo","price_cents":9900}`,
			[]any{"solo", int64(9900), "g-1", lawyer.UserID, at}},
		{"credit cents", GateServiceCredit, serviceCredit, `{"amount_cents":5000,"reason":"downtime"}`,
			[]any{"g-1", int64(5000), "downtime", lawyer.UserID, at}},
		{"import total", GateImportBalances, importBalances, `{"import_id":"imp-1","trust_account_id":"ta-1","total_cents":77}`,
			[]any{"imp-1", "g-1", "ta-1", int64(77)}},
		{"export requested by the user the agent acted for", GateExport, export, `{"scope":"firm","recipient":"a@x"}`,
			[]any{"g-1", `{"scope":"firm","recipient":"a@x"}`, prep.UserID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeExec{}
			g := registry.Approved{GateID: "g-1", Kind: tt.kind, Payload: []byte(tt.pay), Preparer: bot,
				Approvers: []registry.Principal{lawyer}}
			if err := run(context.Background(), tx, g, func() time.Time { return at }, tt.body); err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprint(tx.args[0]); got != fmt.Sprint(tt.want) {
				t.Fatalf("args = %s, want %s", got, fmt.Sprint(tt.want))
			}
		})
	}
}
