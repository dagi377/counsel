package trust

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// FeatureMonthEndReconciliation is the spec id of "month-end-reconciliation".
const FeatureMonthEndReconciliation = "F-TRU-04"

// EventStatementReceived is the event of a trust account's bank statement for a closed month
// being received (docs/QUESTIONS.md Q-FEAT-TRU-04).
const EventStatementReceived = "trust.statement_received"

// GateReconciliationSignOff is G12, recon.GateSignOff. recon imports trust, so the kind is
// restated here; a test holds the two equal.
const GateReconciliationSignOff = "trust.reconciliation_signoff"

// MonthEndReconciliationDef is F-TRU-04: once the bank statement for a closed month is
// received, the agent reads each matter's trust balance and the bills it touches, drafts the
// three-way reconciliation package with draft_document and submits it with
// submit_for_approval for trust.reconciliation_signoff (G12). A lawyer other than the
// preparer signs it off; the agent never signs off a reconciliation.
func MonthEndReconciliationDef() feature.Def {
	return feature.Def{
		ID:      FeatureMonthEndReconciliation,
		Area:    "trust",
		Name:    "month-end-reconciliation",
		Source:  feature.SourceJob,
		Trigger: "schedule (statement received after month end)",
		Actions: []string{"monthly-reconciliation > Prepare package"},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// trust.reconciliation_signoff has no dedicated write tool: the package is drafted and
		// submitted, never signed.
		GateKinds:  []string{GateReconciliationSignOff},
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-tru-04.md",
		Fixture:    "testdata/ai/f-tru-04/month-end-reconciliation",
	}
}

// MonthEndReconciliationBinding binds F-TRU-04 to a received bank statement and to a daily
// check, so a statement that arrived while the firm was closed is still reconciled. The
// scheduled run acts for the firm's designated trust lawyer (JOB-ACTOR); an event run acts
// for the user who filed the statement.
func MonthEndReconciliationBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(MonthEndReconciliationDef(), EventStatementReceived)
}

// dateText is a calendar date as text carries it.
var dateText = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// SourcedCents wraps a trust feature's write tool (draft_document, draft_email) so that, for
// an agent principal, every amount and date its input states is one a read tool returned in
// the same run (pv): a number field named "*_cents", an amount in its text ("125000 cents",
// signed) or a YYYY-MM-DD date in its text. An invented, rounded or converted amount, an
// amount in dollars rather than integer cents ("$1,250.00"), or an overdrawn balance written
// without its sign, is agenttools.ErrUnsourcedValue and the handler never runs, so no draft
// and no gate is made. A user principal (the manual path) is not checked. A nil pv or a tool
// that is not a write tool fails closed with ErrPortMissing.
func SourcedCents(pv *agenttools.Provenance, t registry.Tool) (registry.Tool, error) {
	// rule: AI-R-003
	// Fail closed: a trust write without the run's provenance is never built.
	if pv == nil || t.Kind != registry.ToolWrite || t.Handle == nil {
		return registry.Tool{}, fmt.Errorf("sourced cents %q: %w", t.Name, ErrPortMissing)
	}
	h := t.Handle
	t.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
		if p.Kind == registry.PrincipalAgent {
			if err := checkSourced(pv, p.RunID, in); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", t.Name, err)
			}
		}
		return h(ctx, p, in)
	}
	return t, nil
}

// checkSourced checks every amount and date in a write input against runID's provenance.
// Errors name the field kind only, never the value (it may be a client amount).
func checkSourced(pv *agenttools.Provenance, runID string, in json.RawMessage) error {
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("provenance: %w", agenttools.ErrInvalidInput)
	}
	var stated statedValues
	stated.walk(v, "")
	// rule: ARC-U-027
	// Every amount a trust write states is integer cents: a dollar amount is never checked
	// against the run's figures, it is refused.
	if stated.notCents {
		return fmt.Errorf("amount not in integer cents: %w", agenttools.ErrUnsourcedValue)
	}
	if len(stated.Cents) == 0 && len(stated.Dates) == 0 {
		return nil
	}
	b, err := json.Marshal(stated)
	if err != nil {
		return fmt.Errorf("provenance: %w", err)
	}
	// rule: ARC-U-027
	// rule: BIL-L-036
	// Amounts are signed: an overdrawn ledger read as -5000 cents is never written as 5000,
	// so a written package or request cannot hide an overdraft.
	return pv.Check(runID, []string{"/cents/*", "/dates/*"}, b)
}

// statedValues is every amount and date a write input states.
type statedValues struct {
	Cents    []json.Number `json:"cents"`
	Dates    []string      `json:"dates"`
	notCents bool          // the input states an amount that is not integer cents
}

// walk collects the amounts and dates v states; key is the field v sits in.
func (s *statedValues) walk(v any, key string) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			s.walk(c, k)
		}
	case []any:
		for _, c := range x {
			s.walk(c, key)
		}
	case json.Number:
		if strings.HasSuffix(key, "_cents") {
			s.Cents = append(s.Cents, x)
		}
	case string:
		for _, m := range centsText.FindAllStringSubmatch(x, -1) {
			if n, err := strconv.ParseInt(m[2], 10, 64); err == nil {
				s.Cents = append(s.Cents, json.Number(strconv.FormatInt(signed(m[1], n), 10)))
			} else {
				s.notCents = true // too large to be cents: refused, never checked
			}
		}
		s.Dates = append(s.Dates, dateText.FindAllString(x, -1)...)
		s.notCents = s.notCents || decimalText.MatchString(x)
	}
}
