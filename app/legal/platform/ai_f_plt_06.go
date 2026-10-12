package platform

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-06.md
var promptFPLT06 embed.FS

// FeatureClosingStarter is the spec id of "Closing starter".
const FeatureClosingStarter = "F-PLT-06"

// Closing starter item kinds, one per gate the run drafts for.
const (
	ClosingFinalBill   = "final_bill"        // the final account, for G-B2 (billing.bill_approval)
	ClosingTrustRefund = "trust_refund"      // the trust balance back to the client, for G11 (trust.withdrawal)
	ClosingChecklist   = "closing_checklist" // the closing checklist and letter, for G13 (platform.closing)
)

// ErrClosingRetryQueued means a transient model fault ended the run; a retry is queued and
// nothing was written.
var ErrClosingRetryQueued = errors.New("platform: closing starter model fault, retry queued")

// ErrBadClosingItem means a closing starter item is not usable; the whole output is dropped.
var ErrBadClosingItem = errors.New("platform: closing starter item is not usable")

// closingStarterPrompt is the run's user text. It is fixed: the matter's file travels only as
// quoted triggers.
const closingStarterPrompt = "Start the closing of the matter whose file is quoted below."

// closingStarterSchema is the F-PLT-06 final output: at most one draft per gate, each citing
// the passage it comes from (run.ItemsField, run.CitationField). Amounts are integer cents.
var closingStarterSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "maxItems": 3,
      "items": {
        "type": "object",
        "required": ["kind", "text", "citation"],
        "additionalProperties": false,
        "properties": {
          "kind": {"enum": ["final_bill", "trust_refund", "closing_checklist"]},
          "text": {"type": "string", "minLength": 1, "maxLength": 4000},
          "amount_cents": {"type": "integer", "minimum": 1, "maximum": 100000000000},
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

// ClosingStarterDef is F-PLT-06: when a matter reaches its concluded stage the agent reads its
// file and drafts the three closing steps for people to decide: the final account for G-B2
// (billing.bill_approval), the refund of the trust balance for G11 (trust.withdrawal) and the
// closing checklist for G13 (platform.closing). Each gate is prepared by the acting user; a
// person other than the preparer decides it, the agent never does, and no money moves
// without a lawyer's authorization.
func ClosingStarterDef() feature.Def {
	return feature.Def{
		ID:      FeatureClosingStarter,
		Area:    "platform",
		Name:    "Closing starter",
		Source:  feature.SourceJob,
		Trigger: "event: matter reaches concluded stage, or N days with no activity",
		Kind:    ai.KindToolUse,
		Tools: []string{
			agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument, agent.ToolDraftDocument,
			agent.ToolPrepareTrustWithdrawal, "report_plan", "report_step", agent.ToolSubmitForApproval,
		},
		// rule: ARC-I-035
		// prepare_trust_withdrawal opens G11; G-B2 and G13 are tool-gap kinds a run opens with
		// submit_for_approval on a draft_document of its own (Q-FEAT-PLT-08).
		GateKinds: []string{bill.GateBillApproval, agent.GateTrustWithdrawal, matter.GateClosing},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:        []string{agent.CapMattersView, agent.CapDocumentsView, "documents.edit", agent.CapTrustRequest, agent.CapAsk},
		Prompt:       promptFPLT06,
		PromptPath:   "prompts/f-plt-06.md",
		Fixture:      "testdata/ai/f-plt-06/closing-starter",
		OutputSchema: closingStarterSchema,
	}
}

// ClosingItem is one closing draft. Text is model text: it goes to the sink for a person to
// review and is never logged.
type ClosingItem struct {
	Kind        string // ClosingFinalBill, ClosingTrustRefund or ClosingChecklist
	Text        string // what the draft says: the account, the refund, the checklist
	AmountCents int64  // trust_refund always, final_bill when the file states the total
	Citation    agenttools.Citation
}

// GateKind is the gate the item goes to.
func (c ClosingItem) GateKind() string {
	switch c.Kind {
	case ClosingFinalBill:
		return bill.GateBillApproval
	case ClosingTrustRefund:
		return agent.GateTrustWithdrawal
	default:
		return matter.GateClosing
	}
}

// ClosingSink takes a run's checked items. It is called once per run, with every item, and
// only after the whole output checked out; it opens the gates with the acting user as
// preparer and never decides one.
type ClosingSink interface {
	FileClosing(ctx context.Context, featureID string, items []ClosingItem) error
}

// ClosingStarterPipeline is F-PLT-06's tool-use pipeline.
type ClosingStarterPipeline struct {
	Runner run.Runner
	Sink   ClosingSink
	// Provenance is the set the run's read tools record into (boot wraps them with it); nil
	// means only the quoted file is a source. The file's dates and numbers are added to it
	// for the run.
	Provenance *agenttools.Provenance
}

// Run drives one tool-use run as p (an agent principal acting for the user who prepares)
// over the matter's file, quoted as triggers. Output that fails the schema, cites a source
// neither the triggers nor the run's tool results carried, or carries an amount no source
// stated writes nothing; a transient model fault writes nothing and returns
// ErrClosingRetryQueued.
func (c ClosingStarterPipeline) Run(ctx context.Context, p run.Principal, file []agenttools.Quoted) ([]ClosingItem, error) {
	def := ClosingStarterDef()
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	triggers := make([]run.Trigger, len(file))
	cited := make([]agenttools.Citation, 0, len(file))
	for i, s := range file {
		if err := agenttools.ValidCitation(s.Source); err != nil {
			return nil, fmt.Errorf("%s: passage %d: %w", def.ID, i, err)
		}
		// rule: ARC-I-038
		// The file reaches the model only as quoted trigger data.
		triggers[i] = run.Trigger{Source: s.Source, Text: s.Text}
		cited = append(cited, s.Source)
	}
	out, err := c.Runner.Run(ctx, spec, p, closingStarterPrompt, triggers...)
	if err != nil {
		return nil, err
	}
	if out.RetryQueued {
		return nil, fmt.Errorf("%s: %w", def.ID, ErrClosingRetryQueued)
	}
	pv := c.Provenance
	if pv == nil {
		pv = agenttools.NewProvenance()
	}
	for _, s := range file {
		closingStated(pv, p.RunID, s.Text)
	}
	items, err := decodeClosingItems(out.JSON, append(cited, out.Citations...), pv, p.RunID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if c.Sink == nil {
		return nil, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadClosingItem)
	}
	// rule: AI-R-003
	// Items only open G-B2, G11 and G13; a person other than the preparer decides each.
	if err := c.Sink.FileClosing(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: file: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never draft text, amounts or passages.
	slog.InfoContext(ctx, "closing drafts filed", "feature", def.ID, "items", len(items))
	return items, nil
}

func decodeClosingItems(out json.RawMessage, sources []agenttools.Citation, pv *agenttools.Provenance, runID string) ([]ClosingItem, error) {
	var doc struct {
		Items []struct {
			Kind        string          `json:"kind"`
			Text        string          `json:"text"`
			AmountCents int64           `json:"amount_cents"`
			Citation    closingCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", run.ErrBadOutput)
	}
	seen := map[string]bool{}
	items := make([]ClosingItem, 0, len(doc.Items))
	for i, it := range doc.Items {
		switch it.Kind {
		case ClosingFinalBill, ClosingChecklist, ClosingTrustRefund:
		default:
			return nil, fmt.Errorf("item %d: kind: %w", i, ErrBadClosingItem)
		}
		// One draft per gate: a second refund or a second account is not a closing step.
		if seen[it.Kind] {
			return nil, fmt.Errorf("item %d: second %s: %w", i, it.Kind, ErrBadClosingItem)
		}
		seen[it.Kind] = true
		switch {
		case it.Kind == ClosingTrustRefund && it.AmountCents <= 0:
			// rule: ARC-U-027
			// Fail closed: a refund with no amount moves nothing and drafts nothing.
			return nil, fmt.Errorf("item %d: refund amount: %w", i, ErrBadClosingItem)
		case it.Kind == ClosingChecklist && it.AmountCents != 0:
			return nil, fmt.Errorf("item %d: checklist with an amount: %w", i, ErrBadClosingItem)
		}
		if it.AmountCents != 0 {
			in, err := json.Marshal(map[string]int64{"amount_cents": it.AmountCents})
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			// rule: ARC-U-027
			// The amount is one the file or a read of this run stated, never the model's own.
			if err := pv.Check(runID, []string{"/amount_cents"}, in); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		cite := agenttools.Citation{VersionID: it.Citation.VersionID, Page: it.Citation.Page, FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine}
		if err := agenttools.ValidCitation(cite); err != nil {
			return nil, fmt.Errorf("item %d: %w: %w", i, run.ErrUncitedAnswer, err)
		}
		// rule: ARC-I-038
		// Only a source this run quoted or a tool of this run returned can be cited.
		if !closingCited(sources, cite) {
			return nil, fmt.Errorf("item %d p%d: %w", i, cite.Page, run.ErrUncitedAnswer)
		}
		items = append(items, ClosingItem{Kind: it.Kind, Text: it.Text, AmountCents: it.AmountCents, Citation: cite})
	}
	return items, nil
}

// closingStated records the dates and numbers a quoted passage states as values runID read,
// as the kernel does for any quoted text a read returns.
func closingStated(pv *agenttools.Provenance, runID, text string) {
	if b, err := json.Marshal(agenttools.Quoted{Untrusted: true, Text: text}); err == nil {
		pv.Observe(runID, b)
	}
}

// closingCitation is a model citation in the kernel shape.
type closingCitation struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

// closingCited reports whether c falls inside one of sources: same version and page, lines
// within the source's range.
func closingCited(sources []agenttools.Citation, c agenttools.Citation) bool {
	for _, s := range sources {
		if s.VersionID == c.VersionID && s.Page == c.Page && s.FromLine <= c.FromLine && c.ToLine <= s.ToLine {
			return true
		}
	}
	return false
}
