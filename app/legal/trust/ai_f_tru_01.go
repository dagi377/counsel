package trust

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// The trust AI features (spec/ai/features.json area "trust") live in ai_f_tru_*.go. Each
// exports its feature.Def; app/legal/agent/featureset adds them to the feature.Set, so
// nothing registers from init(). A trust feature only prepares: it never approves,
// authorizes or executes, and every amount it writes is integer cents a read returned.

//go:embed prompts/*.md
var prompts embed.FS

// FeatureBankFeedSync is the spec id of "bank-feed-sync".
const FeatureBankFeedSync = "F-TRU-01"

// MaxBankFeedSources bounds the ranked bank-feed lines one extraction call quotes.
const MaxBankFeedSources = 20

// bankFeedQuery is what bank-feed lines are ranked against. It is fixed feature text and
// never carries client content.
const bankFeedQuery = "trust account deposit, cheque deposited, wire received, electronic funds " +
	"transfer received, retainer, funds in trust, client reference"

// Bank-feed item kinds.
const (
	ItemReceipt = "receipt" // a proposed trust receipt for G6 (trust.receipt_classification)
	ItemMatch   = "match"   // a suggestion that a deposit matches a client reference
)

// Sentinel errors of the trust AI features.
var (
	// ErrPortMissing: a feature port is not wired; the feature fails closed and writes
	// nothing.
	ErrPortMissing = errors.New("trust: feature port not wired")
	// ErrBadReceipt: a proposed receipt is not usable; the whole output is dropped.
	ErrBadReceipt = errors.New("trust: proposed receipt is not usable")
)

// bankFeedSchema is the F-TRU-01 extraction output: proposed receipts and match
// suggestions, each citing the bank-feed line it comes from (run.ItemsField,
// run.CitationField).
var bankFeedSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["kind", "amount_cents", "received_on", "citation"],
        "additionalProperties": false,
        "properties": {
          "kind": {"enum": ["receipt", "match"]},
          "amount_cents": {"type": "integer", "minimum": 1, "maximum": 100000000000},
          "received_on": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "method": {"enum": ["cheque", "wire", "eft", "bank_draft", "cash", "other"]},
          "reference": {"type": "string", "maxLength": 200},
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

// BankFeedSyncDef is F-TRU-01: daily, and when a bank statement is uploaded, the bank-feed
// lines are ranked by embedding and one extraction call proposes trust receipts and match
// suggestions. A receipt only opens trust.receipt_classification (G6) with the acting user
// as preparer; it posts nothing to the trust journal.
func BankFeedSyncDef() feature.Def {
	return feature.Def{
		ID:      FeatureBankFeedSync,
		Area:    "trust",
		Name:    "bank-feed-sync",
		Source:  feature.SourceJob,
		Trigger: "schedule (daily, plus when a statement is uploaded)",
		Actions: []string{
			"trust-receipts-journal > Post proposed receipt from bank feed",
			"bank-feed-import > Create receipt from unmatched deposit",
		},
		// embedding+extraction: bank-feed lines are ranked by embedding, then one extraction
		// call.
		Kind: ai.KindExtraction,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// trust.receipt_classification has no dedicated write tool: a receipt is drafted and
		// submitted, never posted.
		GateKinds:    []string{GateReceiptClassification},
		Grant:        []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-tru-01.md",
		Fixture:      "testdata/ai/f-tru-01/bank-feed-sync",
		OutputSchema: bankFeedSchema,
	}
}

// ProposedReceipt is one proposed trust receipt (ItemReceipt) or match suggestion
// (ItemMatch) read from a bank-feed line. AmountCents is integer cents the cited line
// states; ReceivedOn is the America/Toronto calendar date the line states. Reference is
// bank-feed text: it goes to the sink for a person to review and is never logged.
type ProposedReceipt struct {
	Kind        string
	AmountCents int64
	ReceivedOn  string
	Method      string
	Reference   string
	Citation    agenttools.Citation
}

// ReceiptProposals takes a run's checked items. It is called once per run, with every item,
// and only after the whole output checked out; each receipt opens a
// trust.receipt_classification gate with the acting user as preparer.
type ReceiptProposals interface {
	ProposeReceipts(ctx context.Context, featureID string, items []ProposedReceipt) error
}

// BankFeedPipeline is F-TRU-01's embedding+extraction pipeline.
type BankFeedPipeline struct {
	Loop     *run.Loop
	Embedder ai.Embedder
	Sink     ReceiptProposals
}

// Run ranks the bank-feed lines by embedding, sends the best MaxBankFeedSources to one
// extraction call as quoted sources, and hands the checked items to the Sink. Output that
// fails the schema, cites a line it was not given, or carries an amount or a date its cited
// line does not state writes nothing. A missing port fails closed before any model call.
func (p BankFeedPipeline) Run(ctx context.Context, lines []agenttools.Quoted) ([]ProposedReceipt, error) {
	def := BankFeedSyncDef()
	// rule: AI-R-003
	// Fail closed: without every port the run makes no model call and writes nothing.
	if p.Loop == nil || p.Embedder == nil || p.Sink == nil {
		return nil, fmt.Errorf("%s: %w", def.ID, ErrPortMissing)
	}
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	for i, l := range lines {
		if err := agenttools.ValidCitation(l.Source); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", def.ID, i, err)
		}
	}
	cands := make([]run.Candidate, len(lines))
	for i, l := range lines {
		cands[i] = run.Candidate{ID: fmt.Sprintf("%06d", i), Text: l.Text} // zero-padded: ties rank in feed order
	}
	ranked, err := run.Rank(ctx, p.Embedder, def.ID, bankFeedQuery, cands)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	ranked = ranked[:min(len(ranked), MaxBankFeedSources)]
	best := make([]agenttools.Quoted, len(ranked))
	for i, r := range ranked {
		idx, err := strconv.Atoi(r.ID)
		if err != nil || idx < 0 || idx >= len(lines) {
			return nil, fmt.Errorf("%s: rank id %q: %w", def.ID, r.ID, run.ErrRank)
		}
		best[i] = lines[idx]
	}
	// rule: ARC-I-038
	// Bank-feed lines reach the model only as quoted sources; Extract checks every item
	// cites one of them.
	out, err := p.Loop.Extract(ctx, spec, best)
	if err != nil {
		return nil, err
	}
	items, err := decodeReceipts(out, best)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	// rule: AI-R-003
	// Receipts only open trust.receipt_classification gates; a person other than the
	// preparer classifies and posts them. The agent never posts a receipt.
	if err := p.Sink.ProposeReceipts(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: propose: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never amounts, references or lines.
	slog.InfoContext(ctx, "trust receipts proposed", "feature", def.ID, "items", len(items))
	return items, nil
}

type receiptJSON struct {
	Kind        string `json:"kind"`
	AmountCents int64  `json:"amount_cents"`
	ReceivedOn  string `json:"received_on"`
	Method      string `json:"method"`
	Reference   string `json:"reference"`
	Citation    struct {
		VersionID string `json:"version_id"`
		Page      int    `json:"page"`
		FromLine  int    `json:"from_line"`
		ToLine    int    `json:"to_line"`
	} `json:"citation"`
}

// decodeReceipts reads checked output into items. Every amount and date must be one the
// cited line states; one bad item fails the whole set. Errors carry positions only, never
// model or bank-feed text.
func decodeReceipts(out json.RawMessage, sources []agenttools.Quoted) ([]ProposedReceipt, error) {
	var doc struct {
		Items []receiptJSON `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", ErrBadReceipt)
	}
	items := make([]ProposedReceipt, 0, len(doc.Items))
	for i, it := range doc.Items {
		cite := agenttools.Citation{VersionID: it.Citation.VersionID, Page: it.Citation.Page, FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine}
		src, ok := citedSource(cite, sources)
		if !ok {
			return nil, fmt.Errorf("item %d: %w", i, run.ErrUncitedAnswer)
		}
		// rule: BIL-L-034
		// A trust receipt is money received: a zero or negative amount is not a receipt.
		if it.AmountCents <= 0 {
			return nil, fmt.Errorf("item %d: amount: %w", i, ErrBadReceipt)
		}
		if _, err := time.Parse(time.DateOnly, it.ReceivedOn); err != nil {
			return nil, fmt.Errorf("item %d: received on: %w", i, ErrBadReceipt)
		}
		// rule: ARC-U-027
		// The amount and the date come from the cited bank-feed line, never from the model.
		if !slices.Contains(centsFigures(src.Text), it.AmountCents) {
			return nil, fmt.Errorf("item %d: amount: %w", i, agenttools.ErrUnsourcedValue)
		}
		if !strings.Contains(src.Text, it.ReceivedOn) {
			return nil, fmt.Errorf("item %d: received on: %w", i, agenttools.ErrUnsourcedValue)
		}
		items = append(items, ProposedReceipt{
			Kind: it.Kind, AmountCents: it.AmountCents, ReceivedOn: it.ReceivedOn,
			Method: it.Method, Reference: it.Reference, Citation: cite,
		})
	}
	return items, nil
}

// citedSource returns the quoted source c falls inside: same version and page, lines
// within the source's range.
func citedSource(c agenttools.Citation, sources []agenttools.Quoted) (agenttools.Quoted, bool) {
	if agenttools.ValidCitation(c) != nil {
		return agenttools.Quoted{}, false
	}
	for _, s := range sources {
		if s.Source.VersionID == c.VersionID && s.Source.Page == c.Page && s.Source.FromLine <= c.FromLine && c.ToLine <= s.Source.ToLine {
			return s, true
		}
	}
	return agenttools.Quoted{}, false
}

var (
	// centsText is an amount written as integer cents, "125000 cents", optionally signed.
	centsText = regexp.MustCompile(`(-?)\b(\d+) ?cents\b`)
	// decimalText is an amount written in dollars and cents, "1,250.00" or "$1,250.00",
	// optionally signed.
	decimalText = regexp.MustCompile(`(-?)\$?\b(\d{1,3}(?:,\d{3})+|\d+)\.(\d{2})\b`)
)

// centsFigures returns every amount text states, in signed integer cents: "125000 cents"
// and "$1,250.00" are both 125000; "-5000 cents" is -5000.
func centsFigures(text string) []int64 {
	var out []int64
	for _, m := range centsText.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.ParseInt(m[2], 10, 64); err == nil {
			out = append(out, signed(m[1], n))
		}
	}
	for _, m := range decimalText.FindAllStringSubmatch(text, -1) {
		whole, err := strconv.ParseInt(strings.ReplaceAll(m[2], ",", ""), 10, 64)
		if err != nil {
			continue
		}
		frac, _ := strconv.ParseInt(m[3], 10, 64)
		out = append(out, signed(m[1], whole*100+frac))
	}
	return out
}

func signed(sign string, n int64) int64 {
	if sign == "-" {
		return -n
	}
	return n
}
