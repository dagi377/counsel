package matter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureChronology is the spec id of "Matter chronology builder".
const FeatureChronology = "F-MAT-12"

// GateKeyFact is G-KF: a change to a matter's key facts.
const GateKeyFact = "matter.key_fact"

// Chronology item kinds.
const (
	ItemChronologyRow = "row"      // a proposed row on matter-chronology
	ItemKeyFactCheck  = "key_fact" // a key-fact check proposal for G-KF
)

// MaxChronologySources bounds the ranked passages one chronology extraction call quotes.
const MaxChronologySources = 20

// chronologyQuery is what passages are ranked against. It is fixed feature text and never
// carries client content.
const chronologyQuery = "date, event, meeting, letter sent, notice served, agreement signed, " +
	"incident, payment, filed, hearing, decision"

// chronologySchema is the F-MAT-12 extraction output: chronology rows and key-fact check
// proposals, each citing the quoted passage it came from (run.ItemsField, run.CitationField).
var chronologySchema = json.RawMessage(`{
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
          "kind": {"enum": ["row", "key_fact"]},
          "date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "text": {"type": "string", "minLength": 1, "maxLength": 1000},
          "citation": ` + matCitationSchema + `
        }
      }
    }
  }
}`)

// ChronologyDef is the F-MAT-12 definition, for the featureset to add.
func ChronologyDef() feature.Def {
	return feature.Def{
		ID:      FeatureChronology,
		Area:    "matters",
		Name:    "Matter chronology builder",
		Source:  feature.SourceJob,
		Trigger: "on request; event: new document or email on a matter with a chronology",
		Actions: []string{
			"matter-chronology > Build chronology",
			"matter-chronology > Refresh with new material",
		},
		// embedding+extraction: passages are ranked by embedding, then one extraction call.
		Kind: ai.KindExtraction,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "search_contacts",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// matter.key_fact has no dedicated write tool: the run drafts the check and submits it.
		GateKinds:    []string{GateKeyFact},
		Grant:        []string{"agent.ask", "contacts.view", "documents.edit", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-mat-12.md",
		Fixture:      "testdata/ai/f-mat-12/matter-chronology-builder",
		OutputSchema: chronologySchema,
	}
}

// ChronologyItem is one proposed chronology row or key-fact check (G-KF). Text is model
// text: it goes to the sink for a person to review and is never logged.
type ChronologyItem struct {
	Kind     string // ItemChronologyRow or ItemKeyFactCheck
	Date     string // America/Toronto calendar date, YYYY-MM-DD; required on a row
	Text     string
	Citation agenttools.Citation
}

// ChronologySink takes a run's checked items. It is called once per run, with every item,
// and only after the whole output checked out; rows go to matter-chronology as proposals and
// key-fact checks open G-KF gates with the acting user as preparer.
type ChronologySink interface {
	ProposeChronology(ctx context.Context, featureID string, items []ChronologyItem) error
}

// ChronologyPipeline is F-MAT-12's embedding+extraction pipeline.
type ChronologyPipeline struct {
	Loop     *run.Loop
	Embedder ai.Embedder
	Sink     ChronologySink
}

// Run ranks the matter's passages by embedding, sends the best MaxChronologySources to one
// extraction call as quoted sources, and hands the cited items to the Sink. Model output that
// fails the schema, cites a passage it was not given or carries an unusable date writes
// nothing.
func (c ChronologyPipeline) Run(ctx context.Context, sources []agenttools.Quoted) ([]ChronologyItem, error) {
	def := ChronologyDef()
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
	ranked, err := run.Rank(ctx, c.Embedder, def.ID, chronologyQuery, cands)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	ranked = ranked[:min(len(ranked), MaxChronologySources)]
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
	out, err := c.Loop.Extract(ctx, spec, best)
	if err != nil {
		return nil, err
	}
	items, err := decodeChronology(out)
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
	// Rows are proposals and key-fact checks only open G-KF; a person other than the
	// preparer decides them.
	if err := c.Sink.ProposeChronology(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: propose: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never event text or passages.
	slog.InfoContext(ctx, "chronology proposed", "feature", def.ID, "items", len(items))
	return items, nil
}

// decodeChronology reads checked output into items. A row without a calendar date fails the
// whole set; errors carry positions only, never model text.
func decodeChronology(out json.RawMessage) ([]ChronologyItem, error) {
	var doc struct {
		Items []struct {
			Kind     string      `json:"kind"`
			Date     string      `json:"date"`
			Text     string      `json:"text"`
			Citation matCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", ErrBadItem)
	}
	items := make([]ChronologyItem, 0, len(doc.Items))
	for i, it := range doc.Items {
		if it.Kind != ItemChronologyRow && it.Kind != ItemKeyFactCheck {
			return nil, fmt.Errorf("item %d: kind: %w", i, ErrBadItem)
		}
		if it.Kind == ItemChronologyRow && it.Date == "" {
			return nil, fmt.Errorf("item %d: row without date: %w", i, ErrBadItem)
		}
		if it.Date != "" {
			if _, err := time.Parse(time.DateOnly, it.Date); err != nil {
				return nil, fmt.Errorf("item %d: date: %w", i, ErrBadItem)
			}
		}
		items = append(items, ChronologyItem{
			Kind: it.Kind, Date: it.Date, Text: it.Text,
			Citation: agenttools.Citation{
				VersionID: it.Citation.VersionID, Page: it.Citation.Page,
				FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine,
			},
		})
	}
	return items, nil
}
