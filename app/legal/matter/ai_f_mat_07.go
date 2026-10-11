package matter

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// The matter AI features (spec/ai/features.json area "matters") live in ai_f_mat_*.go. Each
// exports its feature.Def; app/legal/agent/featureset adds them to the feature.Set, so
// nothing registers from init().

//go:embed prompts/*.md
var prompts embed.FS

// FeatureCallSummary is the spec id of "Call and meeting summary".
const FeatureCallSummary = "F-MAT-07"

// Gate kinds the matter AI features open.
const (
	GateAgentNote  = "matter.agent_note"  // G-NT: an agent-drafted note goes on the matter
	GateTimeAccept = "timekeeping.accept" // G9: a proposed time entry is accepted
)

// Call summary item kinds.
const (
	ItemNote      = "note"       // a draft note for G-NT
	ItemTimeEntry = "time_entry" // a proposed time entry for G9
)

// ErrRetryQueued means a transient model fault ended the run; a retry is queued and nothing
// was written.
var ErrRetryQueued = errors.New("matter: model fault, retry queued")

// ErrBadItem means a model item is not usable; the whole output is dropped.
var ErrBadItem = errors.New("matter: model item is not usable")

// callPrompt is the run's user text. It is fixed: the transcript travels only as quoted
// triggers.
const callPrompt = "Summarise the call or meeting transcript quoted below for the matter file."

// matCitationSchema is the kernel citation shape every matter feature item carries.
const matCitationSchema = `{
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

// callSchema is the F-MAT-07 final output: note paragraphs and time entries, each citing
// the transcript passage it comes from (run.ItemsField, run.CitationField).
var callSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["kind", "text", "citation"],
        "additionalProperties": false,
        "properties": {
          "kind": {"enum": ["note", "time_entry"]},
          "text": {"type": "string", "minLength": 1, "maxLength": 4000},
          "work_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "minutes": {"type": "integer", "minimum": 1, "maximum": 1440},
          "citation": ` + matCitationSchema + `
        }
      }
    }
  }
}`)

// CallSummaryDef is the F-MAT-07 definition, for the featureset to add.
func CallSummaryDef() feature.Def {
	return feature.Def{
		ID:      FeatureCallSummary,
		Area:    "matters",
		Name:    "Call and meeting summary",
		Source:  feature.SourceJob,
		Trigger: "event: call recording or transcript filed (consent recorded)",
		Actions: []string{
			"matter-notes-activity > Draft call summary note",
			"matter-notes-activity > Propose time entry from activity",
		},
		Kind: ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_note", "propose_time_entries", "report_plan", "report_step", "submit_for_approval",
		},
		GateKinds:    []string{GateAgentNote, GateTimeAccept},
		Grant:        []string{"agent.ask", "contacts.view", "documents.view", "matters.edit", "matters.view", "time.record"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-mat-07.md",
		Fixture:      "testdata/ai/f-mat-07/call-and-meeting-summary",
		OutputSchema: callSchema,
	}
}

// CallSummaryItem is one draft note paragraph (G-NT) or proposed time entry (G9). Text is
// model text: it goes to the sink for a person to review and is never logged.
type CallSummaryItem struct {
	Kind     string // ItemNote or ItemTimeEntry
	Text     string // the note text, or the time entry narrative
	WorkDate string // time entries: America/Toronto calendar date, YYYY-MM-DD
	Minutes  int    // time entries only
	Citation agenttools.Citation
}

// GateKind is the gate the item goes to.
func (c CallSummaryItem) GateKind() string {
	if c.Kind == ItemTimeEntry {
		return GateTimeAccept
	}
	return GateAgentNote
}

// CallSummarySink takes a run's checked items. It is called once per run, with every item,
// and only after the whole output checked out; it opens gates with the acting user as
// preparer.
type CallSummarySink interface {
	FileCallSummary(ctx context.Context, featureID string, items []CallSummaryItem) error
}

// CallSummaryPipeline is F-MAT-07's tool-use pipeline.
type CallSummaryPipeline struct {
	Runner run.Runner
	Sink   CallSummarySink
}

// Run drives one tool-use run as p (an agent principal acting for the user who prepares)
// over the transcript passages, quoted as triggers. Output that fails the schema, cites a
// source neither the triggers nor the run's tool results carried, or carries an unusable
// time entry writes nothing; a transient model fault writes nothing and returns
// ErrRetryQueued.
func (c CallSummaryPipeline) Run(ctx context.Context, p run.Principal, transcript []agenttools.Quoted) ([]CallSummaryItem, error) {
	def := CallSummaryDef()
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	triggers, cited, err := matTriggers(def.ID, transcript)
	if err != nil {
		return nil, err
	}
	out, err := c.Runner.Run(ctx, spec, p, callPrompt, triggers...)
	if err != nil {
		return nil, err
	}
	if out.RetryQueued {
		return nil, fmt.Errorf("%s: %w", def.ID, ErrRetryQueued)
	}
	items, err := decodeCallSummary(out.JSON, append(cited, out.Citations...))
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
	// Items only open G-NT / G9 gates; a person other than the preparer decides them.
	if err := c.Sink.FileCallSummary(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: file: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never note text or passages.
	slog.InfoContext(ctx, "call summary filed", "feature", def.ID, "items", len(items))
	return items, nil
}

func decodeCallSummary(out json.RawMessage, sources []agenttools.Citation) ([]CallSummaryItem, error) {
	var doc struct {
		Items []struct {
			Kind     string      `json:"kind"`
			Text     string      `json:"text"`
			WorkDate string      `json:"work_date"`
			Minutes  int         `json:"minutes"`
			Citation matCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", run.ErrBadOutput)
	}
	items := make([]CallSummaryItem, 0, len(doc.Items))
	for i, it := range doc.Items {
		switch it.Kind {
		case ItemNote:
			if it.WorkDate != "" || it.Minutes != 0 {
				return nil, fmt.Errorf("item %d: note with time fields: %w", i, ErrBadItem)
			}
		case ItemTimeEntry:
			if it.Minutes <= 0 {
				return nil, fmt.Errorf("item %d: minutes: %w", i, ErrBadItem)
			}
			if _, err := time.Parse(time.DateOnly, it.WorkDate); err != nil {
				return nil, fmt.Errorf("item %d: work date: %w", i, ErrBadItem)
			}
		default:
			return nil, fmt.Errorf("item %d: kind: %w", i, ErrBadItem)
		}
		cite, err := it.Citation.check(sources)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		items = append(items, CallSummaryItem{Kind: it.Kind, Text: it.Text, WorkDate: it.WorkDate, Minutes: it.Minutes, Citation: cite})
	}
	return items, nil
}

// matTriggers turns quoted sources into run triggers and lists their citations.
func matTriggers(featureID string, sources []agenttools.Quoted) ([]run.Trigger, []agenttools.Citation, error) {
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

// matCitation is a model citation in the kernel shape.
type matCitation struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

// check returns c as a kernel citation when it has the kernel shape and falls inside one of
// sources: same version and page, lines within the source's range. Errors carry the
// position only, never model text.
func (c matCitation) check(sources []agenttools.Citation) (agenttools.Citation, error) {
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
