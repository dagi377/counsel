// Package deadlines holds Counsel's deadline AI features (spec/ai/features.json area
// "deadlines"). Each feature file exports its feature.Def; app/legal/boot adds them to the
// feature.Set explicitly, so nothing registers from init().
package deadlines

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/*.md
var prompts embed.FS

// FeatureExtractDeadlines is the spec id of "Extract deadlines from document".
const FeatureExtractDeadlines = "F-DL-01"

// MaxExtractSources bounds the ranked passages one extraction call quotes.
const MaxExtractSources = 12

// extractQuery is what passages are ranked against. It is fixed feature text and never
// carries client content.
const extractQuery = "deadline, due date, time limit, within days of service, hearing date, " +
	"filing deadline, limitation period"

// ErrBadProposal means an extracted deadline is not usable (a date that is not YYYY-MM-DD).
var ErrBadProposal = errors.New("deadlines: extracted deadline is not usable")

// extractSchema is the F-DL-01 extraction output: deadline proposals, each citing the
// quoted passage it came from (run.ItemsField, run.CitationField).
var extractSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["title", "citation"],
        "additionalProperties": false,
        "properties": {
          "title": {"type": "string", "minLength": 1, "maxLength": 200},
          "due_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "trigger_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "period": {"type": "string", "maxLength": 100},
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

// ExtractDeadlinesDef is the F-DL-01 definition, for app/legal/boot to add to its feature.Set.
func ExtractDeadlinesDef() feature.Def {
	return feature.Def{
		ID:      FeatureExtractDeadlines,
		Area:    "deadlines",
		Name:    "Extract deadlines from document",
		Source:  feature.SourceJob,
		Trigger: "event: document filed to matter (order, endorsement, pleading, affidavit of service, notice, correspondence, email)",
		Actions: []string{"matter-deadlines > Extract from document"},
		// embedding+extraction: passages are ranked by embedding, then one extraction call.
		Kind: ai.KindExtraction,
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"propose_deadline", "report_plan", "report_step", "submit_for_approval",
		},
		GateKinds:    []string{"cal.confirm"},
		Grant:        []string{"agent.ask", "deadlines.edit", "deadlines.view", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-dl-01.md",
		Fixture:      "testdata/ai/f-dl-01/extract-deadlines-from-document",
		OutputSchema: extractSchema,
	}
}

// ProposedDeadline is one deadline a feature proposes. Dates are America/Toronto calendar
// dates (YYYY-MM-DD); an empty DueDate means compute_deadline derives it from TriggerDate
// and Period. Title is model text: it goes to the proposer only and is never logged.
type ProposedDeadline struct {
	Title       string
	DueDate     string
	TriggerDate string
	Period      string
	Citation    agenttools.Citation
}

// Proposer opens the gates for proposed deadlines with the acting user as preparer (the
// propose_deadline path). It is called once per run, with every item, and only after the
// whole output checked out.
type Proposer interface {
	ProposeDeadlines(ctx context.Context, featureID string, items []ProposedDeadline) error
}

// ExtractPipeline is F-DL-01's embedding+extraction pipeline.
type ExtractPipeline struct {
	Loop     *run.Loop
	Embedder ai.Embedder
	Proposer Proposer
}

// Run ranks the document passages by embedding, sends the best MaxExtractSources to one
// extraction call as quoted sources, and hands the cited proposals to the Proposer. Model
// output that fails the schema, cites a passage it was not given or carries an unusable date
// writes nothing.
func (p ExtractPipeline) Run(ctx context.Context, sources []agenttools.Quoted) ([]ProposedDeadline, error) {
	def := ExtractDeadlinesDef()
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}
	cands := make([]run.Candidate, len(sources))
	for i, s := range sources {
		cands[i] = run.Candidate{ID: fmt.Sprintf("%06d", i), Text: s.Text} // zero-padded: ties rank in source order
	}
	ranked, err := run.Rank(ctx, p.Embedder, def.ID, extractQuery, cands)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	ranked = ranked[:min(len(ranked), MaxExtractSources)]
	best := make([]agenttools.Quoted, len(ranked))
	for i, r := range ranked {
		idx, err := strconv.Atoi(r.ID)
		if err != nil || idx < 0 || idx >= len(sources) {
			return nil, fmt.Errorf("%s: rank id %q: %w", def.ID, r.ID, run.ErrRank)
		}
		best[i] = sources[idx]
	}
	// rule: ARC-I-038
	// Passages reach the model only as quoted sources; Extract checks every item cites one.
	out, err := p.Loop.Extract(ctx, spec, best)
	if err != nil {
		return nil, err
	}
	items, err := decodeProposals(out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if err := proposeAll(ctx, p.Proposer, def.ID, items); err != nil {
		return nil, err
	}
	return items, nil
}

// proposeAll hands items to proposer. An empty set writes nothing.
func proposeAll(ctx context.Context, proposer Proposer, featureID string, items []ProposedDeadline) error {
	if len(items) == 0 {
		return nil
	}
	if proposer == nil {
		return fmt.Errorf("%s: no proposer: %w", featureID, ErrBadProposal)
	}
	// rule: AI-R-003
	// Proposals only open gates; a person other than the preparer confirms them.
	if err := proposer.ProposeDeadlines(ctx, featureID, items); err != nil {
		return fmt.Errorf("%s: propose: %w", featureID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never titles or passages.
	slog.InfoContext(ctx, "deadlines proposed", "feature", featureID, "items", len(items))
	return nil
}

type proposalJSON struct {
	Title       string `json:"title"`
	DueDate     string `json:"due_date"`
	TriggerDate string `json:"trigger_date"`
	Period      string `json:"period"`
	Citation    struct {
		VersionID string `json:"version_id"`
		Page      int    `json:"page"`
		FromLine  int    `json:"from_line"`
		ToLine    int    `json:"to_line"`
	} `json:"citation"`
}

// decodeProposals reads checked output into proposals. A date that is not a calendar date
// fails the whole set; errors carry positions only, never model text.
func decodeProposals(out json.RawMessage) ([]ProposedDeadline, error) {
	var doc struct {
		Items []proposalJSON `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", ErrBadProposal)
	}
	items := make([]ProposedDeadline, 0, len(doc.Items))
	for i, it := range doc.Items {
		for _, d := range []string{it.DueDate, it.TriggerDate} {
			if d == "" {
				continue
			}
			if _, err := time.Parse(time.DateOnly, d); err != nil {
				return nil, fmt.Errorf("item %d: date: %w", i, ErrBadProposal)
			}
		}
		if it.DueDate == "" && (it.TriggerDate == "" || it.Period == "") {
			return nil, fmt.Errorf("item %d: no due date and no trigger date with period: %w", i, ErrBadProposal)
		}
		items = append(items, ProposedDeadline{
			Title: it.Title, DueDate: it.DueDate, TriggerDate: it.TriggerDate, Period: it.Period,
			Citation: agenttools.Citation{
				VersionID: it.Citation.VersionID, Page: it.Citation.Page,
				FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine,
			},
		})
	}
	return items, nil
}
