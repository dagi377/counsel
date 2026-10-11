package docs

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// The document AI features (spec/ai/features.json area "documents") live in ai_f_doc_*.go.
// Each exports its feature.Def; app/legal/agent/featureset adds them to the feature.Set, so
// nothing registers from init().

//go:embed prompts/*.md
var prompts embed.FS

// FeatureTemplateCurrency is the spec id of "template-and-precedent-currency".
const FeatureTemplateCurrency = "F-DOC-06"

// Gate kinds the document features open.
const (
	GateTemplatePublish = "docs.template_publish" // G-D2: publish a template change
	GatePrecedent       = "docs.precedent"        // G-D3: mark a precedent stale or current
)

// Template-and-precedent-currency item kinds.
const (
	CurrencyChangeRequest = "change_request" // a template needs a change (G-D2)
	CurrencyStaleFlag     = "stale_flag"     // a precedent is stale (G-D3)
)

// ErrRetryQueued means a transient model fault ended the run; a retry is queued and nothing
// was written.
var ErrRetryQueued = errors.New("docs: model fault, retry queued")

// ErrBadItem means a model item is not usable; the whole output is dropped.
var ErrBadItem = errors.New("docs: model item is not usable")

// currencyPrompt is the run's user text. It is fixed: the rule or form change and the
// templates travel only as quoted triggers.
const currencyPrompt = "Review the templates and precedents quoted below against the rule or form change quoted below."

// docCitationSchema is the kernel citation shape every document feature item carries.
const docCitationSchema = `{
      "type": "object",
      "required": ["version_id", "page", "from_line", "to_line"],
      "additionalProperties": false,
      "properties": {
        "version_id": {"type": "string", "minLength": 1},
        "page": {"type": "integer"},
        "from_line": {"type": "integer"},
        "to_line": {"type": "integer"}
      }
    }`

// currencySchema is the F-DOC-06 final output: change requests and stale flags, each naming
// the template or precedent document and citing the passage that makes it out of date.
var currencySchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["kind", "doc_id", "reason", "citation"],
        "additionalProperties": false,
        "properties": {
          "kind": {"enum": ["change_request", "stale_flag"]},
          "doc_id": {"type": "string", "minLength": 1, "maxLength": 100},
          "reason": {"type": "string", "minLength": 1, "maxLength": 1000},
          "citation": ` + docCitationSchema + `
        }
      }
    }
  }
}`)

// TemplateCurrencyDef is the F-DOC-06 definition, for the featureset to add.
func TemplateCurrencyDef() feature.Def {
	return feature.Def{
		ID:      FeatureTemplateCurrency,
		Area:    "documents",
		Name:    "template-and-precedent-currency",
		Source:  feature.SourceJob,
		Trigger: "schedule: weekly, plus event on a known rule or form change",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// Neither kind has a dedicated write tool: the run drafts the change request or the
		// stale flag and submits it.
		GateKinds:    []string{GateTemplatePublish, GatePrecedent},
		Grant:        []string{"agent.ask", "documents.edit", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-doc-06.md",
		Fixture:      "testdata/ai/f-doc-06/template-and-precedent-currency",
		OutputSchema: currencySchema,
	}
}

// CurrencyItem is one change request (G-D2) or stale flag (G-D3). Reason is model text: it
// goes to the sink for a person to review and is never logged.
type CurrencyItem struct {
	Kind     string // CurrencyChangeRequest or CurrencyStaleFlag
	DocID    string // the template or precedent document
	Reason   string
	Citation agenttools.Citation
}

// GateKind is the gate the item goes to.
func (c CurrencyItem) GateKind() string {
	if c.Kind == CurrencyStaleFlag {
		return GatePrecedent
	}
	return GateTemplatePublish
}

// CurrencySink takes a run's checked items. It is called once per run, with every item, and
// only after the whole output checked out; it opens gates with the acting user as preparer.
type CurrencySink interface {
	FlagCurrency(ctx context.Context, featureID string, items []CurrencyItem) error
}

// CurrencyPipeline is F-DOC-06's tool-use pipeline.
type CurrencyPipeline struct {
	Runner run.Runner
	Sink   CurrencySink
}

// Run drives one tool-use run as p (an agent principal acting for the user who prepares)
// over sources (the rule or form change and the template passages), quoted as triggers.
// Output that fails the schema or cites a source neither the triggers nor the run's tool
// results carried writes nothing; a transient model fault writes nothing and returns
// ErrRetryQueued.
func (c CurrencyPipeline) Run(ctx context.Context, p run.Principal, sources []agenttools.Quoted) ([]CurrencyItem, error) {
	def := TemplateCurrencyDef()
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	triggers, cited, err := docTriggers(def.ID, sources)
	if err != nil {
		return nil, err
	}
	out, err := c.Runner.Run(ctx, spec, p, currencyPrompt, triggers...)
	if err != nil {
		return nil, err
	}
	if out.RetryQueued {
		return nil, fmt.Errorf("%s: %w", def.ID, ErrRetryQueued)
	}
	items, err := decodeCurrency(out.JSON, append(cited, out.Citations...))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if c.Sink == nil {
		return nil, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadItem)
	}
	// rule: AI-R-003
	// Items only open G-D2 / G-D3 gates; a person other than the preparer decides them.
	if err := c.Sink.FlagCurrency(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: flag: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never reasons or passages.
	slog.InfoContext(ctx, "template currency flagged", "feature", def.ID, "items", len(items))
	return items, nil
}

// docTriggers turns quoted sources into run triggers and lists their citations.
func docTriggers(featureID string, sources []agenttools.Quoted) ([]run.Trigger, []agenttools.Citation, error) {
	triggers := make([]run.Trigger, len(sources))
	cited := make([]agenttools.Citation, 0, len(sources))
	for i, s := range sources {
		if err := agenttools.ValidCitation(s.Source); err != nil {
			return nil, nil, fmt.Errorf("%s: source %d: %w", featureID, i, err)
		}
		// rule: ARC-I-038
		// Source text reaches the model only as quoted trigger data.
		triggers[i] = run.Trigger{Source: s.Source, Text: s.Text}
		cited = append(cited, s.Source)
	}
	return triggers, cited, nil
}

func decodeCurrency(out json.RawMessage, sources []agenttools.Citation) ([]CurrencyItem, error) {
	var doc struct {
		Items []struct {
			Kind     string      `json:"kind"`
			DocID    string      `json:"doc_id"`
			Reason   string      `json:"reason"`
			Citation docCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", run.ErrBadOutput)
	}
	items := make([]CurrencyItem, 0, len(doc.Items))
	for i, it := range doc.Items {
		if it.Kind != CurrencyChangeRequest && it.Kind != CurrencyStaleFlag {
			return nil, fmt.Errorf("item %d: kind: %w", i, ErrBadItem)
		}
		cite, err := it.Citation.check(sources)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		items = append(items, CurrencyItem{Kind: it.Kind, DocID: it.DocID, Reason: it.Reason, Citation: cite})
	}
	return items, nil
}

// docCitation is a model citation in the kernel shape.
type docCitation struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

// check returns c as a kernel citation when it has the kernel shape and falls inside one of
// sources: same version and page, lines within the source's range. Errors carry the
// position only, never model text.
func (c docCitation) check(sources []agenttools.Citation) (agenttools.Citation, error) {
	cite := agenttools.Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine}
	if err := agenttools.ValidCitation(cite); err != nil {
		return agenttools.Citation{}, fmt.Errorf("%w: %w", run.ErrUncitedAnswer, err)
	}
	// rule: ARC-I-038
	// Only a source this run quoted or a tool of this run returned can be cited.
	for _, s := range sources {
		if s.VersionID == cite.VersionID && s.Page == cite.Page && s.FromLine <= cite.FromLine && cite.ToLine <= s.ToLine {
			return cite, nil
		}
	}
	return agenttools.Citation{}, fmt.Errorf("p%d: %w", cite.Page, run.ErrUncitedAnswer)
}
