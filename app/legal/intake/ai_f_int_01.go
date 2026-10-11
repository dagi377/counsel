package intake

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureTriageNewLead is the spec id of "Triage new lead".
const FeatureTriageNewLead = "F-INT-01"

// GateLeadDecision is the gate kind of spec gate G1: a person decides what happens to a
// lead. It is a tool-gap kind: the run drafts the triage card with draft_document and opens
// it with submit_for_approval.
const GateLeadDecision = "intake.lead_decision"

// Triage item kinds: what one line of the triage card says about the lead.
const (
	TriageSummary          = "summary"
	TriagePracticeArea     = "practice_area"
	TriageUrgency          = "urgency"
	TriageLimitationRisk   = "limitation_risk"
	TriagePossibleConflict = "possible_conflict"
	TriageMissingInfo      = "missing_info"
)

// MaxTriageSources bounds the ranked passages one extraction call quotes.
const MaxTriageSources = 12

// ErrBadTriageItem means the extraction output passed the schema but is not a usable triage
// card: an empty line or a second summary. Nothing is written.
var ErrBadTriageItem = errors.New("intake: bad triage item")

// triageQuery is what lead passages are ranked against. It is fixed feature text and never
// carries client content.
const triageQuery = "legal problem, practice area, opposing party, deadline, limitation period, " +
	"date of incident, urgency, injury, court, amount claimed, documents provided"

// triageSchema is the F-INT-01 extraction output: the lines of the triage card, each citing
// the quoted passage of the lead it came from.
var triageSchema = json.RawMessage(`{
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
          "kind": {"enum": ["summary", "practice_area", "urgency", "limitation_risk", "possible_conflict", "missing_info"]},
          "text": {"type": "string", "minLength": 1, "maxLength": 500},
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

//go:embed prompts/f-int-01.md
var promptFINT01 embed.FS

// TriageNewLeadDef is F-INT-01: when a lead is created the agent ranks the lead's passages
// by embedding, extracts a cited triage card in one extraction call, and the card waits on
// G1 (intake.lead_decision) with the acting user as preparer. A person other than them
// decides the lead.
func TriageNewLeadDef() feature.Def {
	return feature.Def{
		ID:      FeatureTriageNewLead,
		Area:    "intake",
		Name:    "Triage new lead",
		Source:  feature.SourceJob,
		Trigger: "event: lead.created or submission.received",
		// embedding+extraction: passages are ranked by embedding, then one extraction call.
		Kind: ai.KindExtraction,
		Tools: []string{
			"search_contacts", "search_matters", "draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds:    []string{GateLeadDecision},
		Grant:        []string{"agent.ask", "contacts.view", "documents.edit", "matters.view"},
		Prompt:       promptFINT01,
		PromptPath:   "prompts/f-int-01.md",
		Fixture:      "testdata/ai/f-int-01/triage-new-lead",
		OutputSchema: triageSchema,
	}
}

// TriageNewLeadBinding binds F-INT-01 to lead.created. Event runs act for the event's user.
func TriageNewLeadBinding() (trigger.Binding, error) {
	return trigger.Bind(TriageNewLeadDef(), "", EventLeadCreated)
}

// TriageItem is one line of the triage card. Text is model text: it goes to the sink only
// and is never logged.
type TriageItem struct {
	Kind     string
	Text     string
	Citation agenttools.Citation
}

// TriageCard is one run's checked triage of a lead.
type TriageCard struct {
	LeadID string
	Items  []TriageItem
}

// TriageSink drafts the triage card on the lead and opens G1 (intake.lead_decision) on it,
// with the acting user as preparer. It is called once per run, only after the whole output
// checked out.
type TriageSink interface {
	Triage(ctx context.Context, featureID string, card TriageCard) error
}

// TriagePipeline is F-INT-01's embedding+extraction pipeline.
type TriagePipeline struct {
	Loop     *run.Loop
	Embedder ai.Embedder
	Sink     TriageSink
}

// Run ranks the lead's passages by embedding, sends the best MaxTriageSources to one
// extraction call as quoted sources, and hands the cited triage card to the Sink. Output that
// fails the schema, cites a passage it was not given, holds an empty line or two summaries
// writes nothing.
func (p TriagePipeline) Run(ctx context.Context, leadID string, sources []agenttools.Quoted) (TriageCard, error) {
	def := TriageNewLeadDef()
	spec, err := def.Spec()
	if err != nil {
		return TriageCard{}, err
	}
	best, err := rankTriageSources(ctx, p.Embedder, sources)
	if err != nil || len(best) == 0 {
		return TriageCard{}, err
	}
	// rule: ARC-I-038
	// Passages reach the model only as quoted sources; Extract checks every item cites one.
	out, err := p.Loop.Extract(ctx, spec, best)
	if err != nil {
		return TriageCard{}, err
	}
	items, err := decodeTriage(out)
	if err != nil {
		return TriageCard{}, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return TriageCard{}, nil
	}
	if p.Sink == nil {
		return TriageCard{}, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadTriageItem)
	}
	card := TriageCard{LeadID: leadID, Items: items}
	// rule: AI-R-003
	// The card opens G1; a person other than the preparer decides the lead.
	if err := p.Sink.Triage(ctx, def.ID, card); err != nil {
		return TriageCard{}, fmt.Errorf("%s: triage: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id, the lead id and a count are logged, never lead text.
	slog.InfoContext(ctx, "lead triaged", "feature", def.ID, "lead", leadID, "items", len(items))
	return card, nil
}

// rankTriageSources returns the best MaxTriageSources of sources by embedding similarity to
// triageQuery.
func rankTriageSources(ctx context.Context, e ai.Embedder, sources []agenttools.Quoted) ([]agenttools.Quoted, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	cands := make([]run.Candidate, len(sources))
	for i, s := range sources {
		cands[i] = run.Candidate{ID: fmt.Sprintf("%06d", i), Text: s.Text} // zero-padded: ties rank in source order
	}
	ranked, err := run.Rank(ctx, e, FeatureTriageNewLead, triageQuery, cands)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", FeatureTriageNewLead, err)
	}
	ranked = ranked[:min(len(ranked), MaxTriageSources)]
	best := make([]agenttools.Quoted, len(ranked))
	for i, r := range ranked {
		idx, err := strconv.Atoi(r.ID)
		if err != nil || idx < 0 || idx >= len(sources) {
			return nil, fmt.Errorf("%s: rank id %q: %w", FeatureTriageNewLead, r.ID, run.ErrRank)
		}
		best[i] = sources[idx]
	}
	return best, nil
}

// decodeTriage reads checked output. Errors carry positions only, never model text.
func decodeTriage(out json.RawMessage) ([]TriageItem, error) {
	var doc struct {
		Items []struct {
			Kind     string `json:"kind"`
			Text     string `json:"text"`
			Citation struct {
				VersionID string `json:"version_id"`
				Page      int    `json:"page"`
				FromLine  int    `json:"from_line"`
				ToLine    int    `json:"to_line"`
			} `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", ErrBadTriageItem)
	}
	items := make([]TriageItem, 0, len(doc.Items))
	summaries := 0
	for i, it := range doc.Items {
		if len(bytes.TrimSpace([]byte(it.Text))) == 0 {
			return nil, fmt.Errorf("item %d: empty: %w", i, ErrBadTriageItem)
		}
		if it.Kind == TriageSummary {
			if summaries++; summaries > 1 {
				return nil, fmt.Errorf("item %d: second summary: %w", i, ErrBadTriageItem)
			}
		}
		c := it.Citation
		items = append(items, TriageItem{Kind: it.Kind, Text: it.Text,
			Citation: agenttools.Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine}})
	}
	return items, nil
}
