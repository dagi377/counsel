package platform

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-04.md
var promptFPLT04 embed.FS

// FeatureCallSummarizer is the spec id of "Call summarizer".
const FeatureCallSummarizer = "F-PLT-04"

// GateSummaryAccuracy is G-SUM: a person confirms an agent's call summary is accurate before
// it goes on the matter.
const GateSummaryAccuracy = "platform.summary_accuracy"

// Call summarizer item kinds.
const (
	CallSummary   = "summary"    // a summary paragraph, for G-SUM
	CallTask      = "task"       // a follow-up the call agreed on, reviewed with the summary (G-SUM)
	CallTimeEntry = "time_entry" // the lawyer's time on the call, for G9
)

// ErrCallRetryQueued means a transient model fault ended the run; a retry is queued and
// nothing was written.
var ErrCallRetryQueued = errors.New("platform: call summarizer model fault, retry queued")

// ErrBadCallItem means a call summarizer item is not usable; the whole output is dropped.
var ErrBadCallItem = errors.New("platform: call summarizer item is not usable")

// callSummarizerPrompt is the run's user text. It is fixed: the transcript travels only as
// quoted triggers.
const callSummarizerPrompt = "Summarize the call transcript quoted below for the matter file."

// callSummarizerSchema is the F-PLT-04 final output: summary paragraphs, follow-up tasks and
// time entries, each citing the passage it comes from (run.ItemsField, run.CitationField).
var callSummarizerSchema = json.RawMessage(`{
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
          "kind": {"enum": ["summary", "task", "time_entry"]},
          "text": {"type": "string", "minLength": 1, "maxLength": 4000},
          "work_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "minutes": {"type": "integer", "minimum": 1, "maximum": 1440},
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

// CallSummarizerDef is F-PLT-04: on request from the comms-log, one tool-use run over a call
// transcript filed with the participants' recorded consent drafts the summary and the
// follow-up tasks for G-SUM (platform.summary_accuracy) and the lawyer's time for G9
// (timekeeping.accept). It binds no event: F-MAT-07 owns the transcript-filed event
// (ai_f_plt_04_write.go). Each gate is prepared by the acting user; a person other than the
// preparer decides it and the agent never does.
func CallSummarizerDef() feature.Def {
	return feature.Def{
		ID:      FeatureCallSummarizer,
		Area:    "platform",
		Name:    "Call summarizer",
		Source:  feature.SourceJob,
		Trigger: "on request from comms-log",
		Actions: []string{ActionSummarizeCall},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "search_documents", "read_document",
			"draft_note", "draft_document", "propose_time_entries", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// G-SUM is a tool-gap kind (Q-FEAT-PLT-06): draft_document stores the summary and
		// submit_for_approval opens G-SUM on it; propose_time_entries opens G9. draft_note
		// stays as the spec lists it, but it opens matter.agent_note, which this run does not
		// hold, so the scoped opener refuses it.
		GateKinds: []string{GateSummaryAccuracy, agent.GateTimeAccept},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:        []string{"agent.ask", "documents.edit", "documents.view", "matters.edit", "matters.view", "time.record"},
		Prompt:       promptFPLT04,
		PromptPath:   "prompts/f-plt-04.md",
		Fixture:      "testdata/ai/f-plt-04/call-summarizer",
		OutputSchema: callSummarizerSchema,
	}
}

// CallItem is one summary paragraph or follow-up task (G-SUM) or proposed time entry (G9).
// Text is model text: it goes to the sink for a person to review and is never logged.
type CallItem struct {
	Kind     string // CallSummary, CallTask or CallTimeEntry
	Text     string // the paragraph, the task, or the time entry narrative
	WorkDate string // time entries: America/Toronto calendar date, YYYY-MM-DD
	Minutes  int    // time entries only
	Citation agenttools.Citation
}

// GateKind is the gate the item goes to.
func (c CallItem) GateKind() string {
	if c.Kind == CallTimeEntry {
		return agent.GateTimeAccept
	}
	return GateSummaryAccuracy
}

// CallSummarySink takes a run's checked items. It is called once per run, with every item,
// and only after the whole output checked out; it opens the gates with the acting user as
// preparer.
type CallSummarySink interface {
	FileCallSummary(ctx context.Context, featureID string, items []CallItem) error
}

// CallSummarizerPipeline is F-PLT-04's tool-use pipeline.
type CallSummarizerPipeline struct {
	Runner run.Runner
	Sink   CallSummarySink
	// Provenance is the set the run's read tools record into (boot wraps them with it); nil
	// means only the transcript is a source. The transcript's dates and numbers are added to
	// it for the run.
	Provenance *agenttools.Provenance
}

// callNumber finds the calendar dates and whole numbers a transcript passage states.
var callNumber = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d+`)

// Run drives one tool-use run as p (an agent principal acting for the user who prepares)
// over the transcript passages, quoted as triggers. Output that fails the schema, cites a
// source neither the triggers nor the run's tool results carried, or carries a time entry
// whose date or minutes no source stated writes nothing; a transient model fault writes
// nothing and returns ErrCallRetryQueued.
func (c CallSummarizerPipeline) Run(ctx context.Context, p run.Principal, transcript []agenttools.Quoted) ([]CallItem, error) {
	def := CallSummarizerDef()
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	triggers := make([]run.Trigger, len(transcript))
	cited := make([]agenttools.Citation, 0, len(transcript))
	for i, s := range transcript {
		if err := agenttools.ValidCitation(s.Source); err != nil {
			return nil, fmt.Errorf("%s: passage %d: %w", def.ID, i, err)
		}
		// rule: ARC-I-038
		// Transcript text reaches the model only as quoted trigger data.
		triggers[i] = run.Trigger{Source: s.Source, Text: s.Text}
		cited = append(cited, s.Source)
	}
	out, err := c.Runner.Run(ctx, spec, p, callSummarizerPrompt, triggers...)
	if err != nil {
		return nil, err
	}
	if out.RetryQueued {
		return nil, fmt.Errorf("%s: %w", def.ID, ErrCallRetryQueued)
	}
	pv := c.Provenance
	if pv == nil {
		pv = agenttools.NewProvenance()
	}
	for _, s := range transcript {
		observeStated(pv, p.RunID, s.Text)
	}
	items, err := decodeCallItems(out.JSON, append(cited, out.Citations...), pv, p.RunID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if c.Sink == nil {
		return nil, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadCallItem)
	}
	// rule: AI-R-003
	// Items only open G-SUM / G9 gates; a person other than the preparer decides them.
	if err := c.Sink.FileCallSummary(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: file: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never summary text or passages.
	slog.InfoContext(ctx, "call summary filed", "feature", def.ID, "items", len(items))
	return items, nil
}

// observeStated records the dates and whole numbers text states as values runID read.
func observeStated(pv *agenttools.Provenance, runID, text string) {
	var vals []any
	for _, s := range callNumber.FindAllString(text, -1) {
		if len(s) == len("2006-01-02") && s[4] == '-' {
			vals = append(vals, s)
			continue
		}
		vals = append(vals, json.Number(s))
	}
	if b, err := json.Marshal(vals); err == nil && len(vals) > 0 {
		pv.Observe(runID, b)
	}
}

// callCitation is a model citation in the kernel shape.
type callCitation struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

func decodeCallItems(out json.RawMessage, sources []agenttools.Citation, pv *agenttools.Provenance, runID string) ([]CallItem, error) {
	var doc struct {
		Items []struct {
			Kind     string       `json:"kind"`
			Text     string       `json:"text"`
			WorkDate string       `json:"work_date"`
			Minutes  int          `json:"minutes"`
			Citation callCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", run.ErrBadOutput)
	}
	items := make([]CallItem, 0, len(doc.Items))
	for i, it := range doc.Items {
		switch it.Kind {
		case CallSummary, CallTask:
			if it.WorkDate != "" || it.Minutes != 0 {
				return nil, fmt.Errorf("item %d: %s with time fields: %w", i, it.Kind, ErrBadCallItem)
			}
		case CallTimeEntry:
			if it.Minutes <= 0 {
				return nil, fmt.Errorf("item %d: minutes: %w", i, ErrBadCallItem)
			}
			if _, err := time.Parse(time.DateOnly, it.WorkDate); err != nil {
				return nil, fmt.Errorf("item %d: work date: %w", i, ErrBadCallItem)
			}
			in, err := json.Marshal(map[string]any{"work_date": it.WorkDate, "minutes": it.Minutes})
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			// rule: ARC-U-027
			// The date and minutes come from the transcript or a read of this run, never the
			// model's own.
			if err := pv.Check(runID, []string{"/work_date", "/minutes"}, in); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		default:
			return nil, fmt.Errorf("item %d: kind: %w", i, ErrBadCallItem)
		}
		cite := agenttools.Citation{VersionID: it.Citation.VersionID, Page: it.Citation.Page, FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine}
		if err := agenttools.ValidCitation(cite); err != nil {
			return nil, fmt.Errorf("item %d: %w: %w", i, run.ErrUncitedAnswer, err)
		}
		// rule: ARC-I-038
		// Only a source this run quoted or a tool of this run returned can be cited.
		if !callCited(sources, cite) {
			return nil, fmt.Errorf("item %d p%d: %w", i, cite.Page, run.ErrUncitedAnswer)
		}
		items = append(items, CallItem{Kind: it.Kind, Text: it.Text, WorkDate: it.WorkDate, Minutes: it.Minutes, Citation: cite})
	}
	return items, nil
}

// callCited reports whether c falls inside one of sources: same version and page, lines
// within the source's range.
func callCited(sources []agenttools.Citation, c agenttools.Citation) bool {
	for _, s := range sources {
		if s.VersionID == c.VersionID && s.Page == c.Page && s.FromLine <= c.FromLine && c.ToLine <= s.ToLine {
			return true
		}
	}
	return false
}
