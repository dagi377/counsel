package trust

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeaturePayeeFraudCheck is the spec id of "payee-fraud-check".
const FeaturePayeeFraudCheck = "F-TRU-03"

// EventReceivingAccountChanged is the trust event published when a withdrawal payee's
// receiving account is added or changed. It starts F-TRU-03. The a2 event list has no such
// event yet (docs/QUESTIONS.md Q-FEAT-TRU-11).
const EventReceivingAccountChanged = "trust.receiving_account_changed"

//go:embed prompts/f-tru-03.md
var promptFTRU03 embed.FS

// PayeeVerdict is the outcome of a payee fraud check on the withdrawals it covers.
type PayeeVerdict string

// Payee verdicts. Block is the default: anything short of a clean, sourced pass blocks.
const (
	PayeeBlock PayeeVerdict = "block"
	PayeePass  PayeeVerdict = "pass"
)

// Payee flag reasons: a closed set, so a flag carries no model-written text.
const (
	FlagAccountChanged     = "account_changed_before_withdrawal"
	FlagPayeeNotContact    = "payee_not_matter_contact"
	FlagAmountNotOnBill    = "amount_not_on_bill"
	FlagBillNotDelivered   = "bill_not_delivered"
	FlagExceedsTrust       = "exceeds_trust_balance"
	FlagTrustOverdrawn     = "trust_overdrawn"
	FlagDetailsMismatch    = "account_details_mismatch"
	FlagUnusualPayeeChange = "unusual_payee_change"
)

var payeeFlagReasons = []string{
	FlagAccountChanged, FlagPayeeNotContact, FlagAmountNotOnBill, FlagBillNotDelivered,
	FlagExceedsTrust, FlagTrustOverdrawn, FlagDetailsMismatch, FlagUnusualPayeeChange,
}

// ErrBadPayeeCheck means the F-TRU-03 output is not usable; the check blocks.
var ErrBadPayeeCheck = errors.New("trust: payee check output is not usable")

// payeeCheckSchema is the F-TRU-03 extraction output: a verdict and ranked flag rows, each
// citing a source a read returned in the run (run.ItemsField, run.CitationField).
var payeeCheckSchema = json.RawMessage(`{
  "type": "object",
  "required": ["verdict", "items"],
  "additionalProperties": false,
  "properties": {
    "verdict": {"enum": ["block", "pass"]},
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["rank", "reason", "citation"],
        "additionalProperties": false,
        "properties": {
          "rank": {"type": "integer", "minimum": 1},
          "reason": {"enum": ["account_changed_before_withdrawal", "payee_not_matter_contact",
            "amount_not_on_bill", "bill_not_delivered", "exceeds_trust_balance", "trust_overdrawn",
            "account_details_mismatch", "unusual_payee_change"]},
          "matter_id": {"type": "string"},
          "bill_id": {"type": "string"},
          "amount_cents": {"type": "integer"},
          "citation": {
            "type": "object",
            "required": ["version_id", "page", "from_line", "to_line"],
            "additionalProperties": false,
            "properties": {
              "version_id": {"type": "string", "minLength": 1},
              "page": {"type": "integer"},
              "from_line": {"type": "integer"},
              "to_line": {"type": "integer"}
            }
          }
        }
      }
    }
  }
}`)

// PayeeFraudCheckDef is F-TRU-03: when a payee's receiving account is added or changed, the
// agent reads the matter, its trust balance and its bills and returns block or pass for the
// withdrawal checks, with ranked, cited flag rows. It is read-only: no write tool, no gate.
// The result only blocks or passes the check a person runs; it moves no money.
func PayeeFraudCheckDef() feature.Def {
	return feature.Def{
		ID:      FeaturePayeeFraudCheck,
		Area:    "trust",
		Name:    "payee-fraud-check",
		Source:  feature.SourceJob,
		Trigger: "event (new or changed receiving account)",
		Kind:    ai.KindExtraction,
		Tools:   []string{"get_matter", "get_trust_balance", "get_bill"},
		// rule: AI-R-003
		// Read tools only: the run's agent principal holds their capabilities and no other.
		Grant:        []string{"billing.view", "matters.view", "trust.view"},
		Prompt:       promptFTRU03,
		PromptPath:   "prompts/f-tru-03.md",
		Fixture:      "testdata/ai/f-tru-03/payee-fraud-check",
		OutputSchema: payeeCheckSchema,
	}
}

// PayeeFraudCheckBinding binds F-TRU-03 to a new or changed receiving account. Event runs act
// for the event's user, so it needs no scheduled acting user.
func PayeeFraudCheckBinding() (trigger.Binding, error) {
	return trigger.Bind(PayeeFraudCheckDef(), "", EventReceivingAccountChanged)
}

// PayeeFlag is one ranked reason to block. AmountCents is set only when the row names an
// amount, and then it is a figure a read returned in the run.
type PayeeFlag struct {
	Rank        int
	Reason      string
	MatterID    string
	BillID      string
	AmountCents *int64
	Citation    agenttools.Citation
}

// PayeeCheck is the checked F-TRU-03 result.
type PayeeCheck struct {
	Verdict PayeeVerdict
	Flags   []PayeeFlag // overdrawn-ledger flags first, then by rank
}

type payeeFlagJSON struct {
	Rank        int    `json:"rank"`
	Reason      string `json:"reason"`
	MatterID    string `json:"matter_id"`
	BillID      string `json:"bill_id"`
	AmountCents *int64 `json:"amount_cents"`
	Citation    struct {
		VersionID string `json:"version_id"`
		Page      int    `json:"page"`
		FromLine  int    `json:"from_line"`
		ToLine    int    `json:"to_line"`
	} `json:"citation"`
}

// CheckPayeeFraud reads the final output of an F-TRU-03 run (out.JSON, else out.Text) and
// checks it: every flag cites a source a read of this run returned (out.Citations), every
// amount is a figure a read of run runID returned (pv), and ranks are distinct. It fails
// closed: on any error the result is a block. A pass stands only when the model passed and
// raised no flag. Errors carry positions, never output text.
func CheckPayeeFraud(ctx context.Context, out run.Outcome, pv *agenttools.Provenance, runID string) (PayeeCheck, error) {
	blocked := PayeeCheck{Verdict: PayeeBlock}
	// rule: ARC-U-027
	// Fail closed: without the run's read values no amount can be shown to be sourced.
	if pv == nil || runID == "" {
		return blocked, fmt.Errorf("%s: no provenance: %w", FeaturePayeeFraudCheck, agenttools.ErrUnsourcedValue)
	}
	raw := bytes.TrimSpace(out.JSON)
	if len(raw) == 0 {
		raw = bytes.TrimSpace([]byte(out.Text))
	}
	var doc struct {
		Verdict PayeeVerdict    `json:"verdict"`
		Items   []payeeFlagJSON `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil || doc.Items == nil {
		return blocked, fmt.Errorf("%s: decode output: %w", FeaturePayeeFraudCheck, ErrBadPayeeCheck)
	}
	if doc.Verdict != PayeeBlock && doc.Verdict != PayeePass {
		return blocked, fmt.Errorf("%s: verdict: %w", FeaturePayeeFraudCheck, ErrBadPayeeCheck)
	}
	// rule: ARC-U-027
	// Every amount a flag names is a figure the run read, never one the model worked out.
	if err := pv.Check(runID, []string{"/items/*/amount_cents"}, raw); err != nil {
		return blocked, fmt.Errorf("%s: %w", FeaturePayeeFraudCheck, err)
	}
	flags := make([]PayeeFlag, 0, len(doc.Items))
	ranks := map[int]bool{}
	for i, it := range doc.Items {
		if it.Rank < 1 || ranks[it.Rank] || !slices.Contains(payeeFlagReasons, it.Reason) {
			return blocked, fmt.Errorf("%s: item %d: %w", FeaturePayeeFraudCheck, i, ErrBadPayeeCheck)
		}
		ranks[it.Rank] = true
		c := agenttools.Citation{VersionID: it.Citation.VersionID, Page: it.Citation.Page, FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine}
		// rule: ARC-I-038
		// A flag cites only a source a read of this run returned.
		if err := agenttools.ValidCitation(c); err != nil || !citedInRun(out.Citations, c) {
			return blocked, fmt.Errorf("%s: item %d: %w", FeaturePayeeFraudCheck, i, run.ErrUncitedAnswer)
		}
		flags = append(flags, PayeeFlag{Rank: it.Rank, Reason: it.Reason, MatterID: it.MatterID, BillID: it.BillID, AmountCents: it.AmountCents, Citation: c})
	}
	// rule: BIL-I-010
	// An overdrawn ledger is never hidden: its flags lead the list whatever rank the model gave.
	slices.SortStableFunc(flags, func(a, b PayeeFlag) int {
		ao, bo := a.Reason == FlagTrustOverdrawn, b.Reason == FlagTrustOverdrawn
		switch {
		case ao && !bo:
			return -1
		case bo && !ao:
			return 1
		}
		return a.Rank - b.Rank
	})
	res := PayeeCheck{Verdict: PayeeBlock, Flags: flags}
	// rule: AI-R-003
	// Any flag blocks: a pass with a flag is a block, never a silent pass.
	if doc.Verdict == PayeePass && len(flags) == 0 {
		res.Verdict = PayeePass
	}
	// rule: AI-R-005
	// Only the feature, the verdict and the count are logged, never ids, amounts or text.
	slog.InfoContext(ctx, "payee check", "feature", FeaturePayeeFraudCheck, "verdict", string(res.Verdict), "flags", len(flags))
	return res, nil
}

// citedInRun reports whether c falls inside a citation the run's reads returned.
func citedInRun(cites []agenttools.Citation, c agenttools.Citation) bool {
	for _, r := range cites {
		if r.VersionID == c.VersionID && r.Page == c.Page && r.FromLine <= c.FromLine && c.ToLine <= r.ToLine {
			return true
		}
	}
	return false
}
