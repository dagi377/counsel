package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureAutoFile is the spec id of "Auto-file".
const FeatureAutoFile = "F-DOC-13"

// MaxRankedSources bounds the ranked passages one extraction call quotes.
const MaxRankedSources = 12

// AutoFileReviewBelowPPM is the confidence under which an auto-file suggestion is queued
// for review instead of offered as ready to file.
const AutoFileReviewBelowPPM = 800_000

// autoFileQuery is what passages are ranked against. It is fixed feature text and never
// carries client content.
const autoFileQuery = "document title, document type, court form number, letter, memo, " +
	"precedent, engagement letter, date, parties, heading"

// autoFileSchema is the F-DOC-13 extraction output: filing suggestions, each citing the
// quoted passage it came from (run.ItemsField, run.CitationField).
var autoFileSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "maxItems": 1,
      "items": {
        "type": "object",
        "required": ["name", "kind", "confidence_ppm", "citation"],
        "additionalProperties": false,
        "properties": {
          "name": {"type": "string", "minLength": 1, "maxLength": 200},
          "kind": {"type": "string", "minLength": 1, "maxLength": 50},
          "folder": {"type": "string", "maxLength": 200},
          "confidence_ppm": {"type": "integer", "minimum": 0, "maximum": 1000000},
          "citation": ` + docCitationSchema + `
        }
      }
    }
  }
}`)

// AutoFileDef is the F-DOC-13 definition, for the featureset to add. It is read-only: it
// opens no gate and offers no write tool.
func AutoFileDef() feature.Def {
	return feature.Def{
		ID:      FeatureAutoFile,
		Area:    "documents",
		Name:    "Auto-file",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionAutoFile,
		Actions: []string{ActionAutoFile},
		// embedding+extraction: passages are ranked by embedding, then one extraction call.
		Kind:         ai.KindExtraction,
		Tools:        []string{"get_matter", "list_documents", "read_document", "search_documents"},
		Grant:        []string{"agent.ask", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-doc-13.md",
		Fixture:      "testdata/ai/f-doc-13/auto-file",
		OutputSchema: autoFileSchema,
	}
}

// FilingSuggestion is how Auto-file proposes to name, classify and file a document. Name and
// Folder are model text: they go to the screen only and are never logged.
type FilingSuggestion struct {
	Name          string
	Kind          string // one of Kinds()
	Folder        string
	ConfidencePPM int
	Review        bool // confidence under AutoFileReviewBelowPPM: queued for review
	Citation      agenttools.Citation
}

// FilingSink shows a run's checked suggestions on matter-documents. It is called once per
// run, only after the whole output checked out; the user may ignore every suggestion.
type FilingSink interface {
	SuggestFiling(ctx context.Context, featureID string, items []FilingSuggestion) error
}

// AutoFilePipeline is F-DOC-13's embedding+extraction pipeline.
type AutoFilePipeline struct {
	Loop     *run.Loop
	Embedder ai.Embedder
	Sink     FilingSink
}

// Run ranks the document passages by embedding, sends the best MaxRankedSources to one
// extraction call as quoted sources, and hands the cited suggestion to the Sink. Model output
// that fails the schema, cites a passage it was not given or names an unknown kind writes
// nothing.
func (a AutoFilePipeline) Run(ctx context.Context, sources []agenttools.Quoted) ([]FilingSuggestion, error) {
	def := AutoFileDef()
	spec, err := def.Spec()
	if err != nil {
		return nil, err
	}
	best, err := docRank(ctx, a.Embedder, def.ID, autoFileQuery, sources)
	if err != nil || len(best) == 0 {
		return nil, err
	}
	// rule: ARC-I-038
	// Passages reach the model only as quoted sources; Extract checks every item cites one.
	out, err := a.Loop.Extract(ctx, spec, best)
	if err != nil {
		return nil, err
	}
	items, err := decodeFilings(out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if a.Sink == nil {
		return nil, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadItem)
	}
	// rule: AI-R-003
	// A suggestion changes nothing: the user files the document or ignores it.
	if err := a.Sink.SuggestFiling(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: suggest: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never names or passages.
	slog.InfoContext(ctx, "auto-file suggested", "feature", def.ID, "items", len(items))
	return items, nil
}

// docRank returns the MaxRankedSources sources that rank best against query, best first.
// No sources means no embedding call and no result.
func docRank(ctx context.Context, e ai.Embedder, featureID, query string, sources []agenttools.Quoted) ([]agenttools.Quoted, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	cands := make([]run.Candidate, len(sources))
	for i, s := range sources {
		cands[i] = run.Candidate{ID: fmt.Sprintf("%06d", i), Text: s.Text} // zero-padded: ties rank in source order
	}
	ranked, err := run.Rank(ctx, e, featureID, query, cands)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", featureID, err)
	}
	ranked = ranked[:min(len(ranked), MaxRankedSources)]
	best := make([]agenttools.Quoted, len(ranked))
	for i, r := range ranked {
		idx, err := strconv.Atoi(r.ID)
		if err != nil || idx < 0 || idx >= len(sources) {
			return nil, fmt.Errorf("%s: rank id %q: %w", featureID, r.ID, run.ErrRank)
		}
		best[i] = sources[idx]
	}
	return best, nil
}

// decodeFilings reads checked output into suggestions. An unknown kind fails the whole set;
// errors carry positions only, never model text.
func decodeFilings(out json.RawMessage) ([]FilingSuggestion, error) {
	var doc struct {
		Items []struct {
			Name          string      `json:"name"`
			Kind          string      `json:"kind"`
			Folder        string      `json:"folder"`
			ConfidencePPM int         `json:"confidence_ppm"`
			Citation      docCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode output: %w", ErrBadItem)
	}
	items := make([]FilingSuggestion, 0, len(doc.Items))
	for i, it := range doc.Items {
		if !knownKind(it.Kind) {
			return nil, fmt.Errorf("item %d: kind: %w", i, ErrBadItem)
		}
		items = append(items, FilingSuggestion{
			Name: it.Name, Kind: it.Kind, Folder: it.Folder, ConfidencePPM: it.ConfidencePPM,
			Review: it.ConfidencePPM < AutoFileReviewBelowPPM,
			Citation: agenttools.Citation{
				VersionID: it.Citation.VersionID, Page: it.Citation.Page,
				FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine,
			},
		})
	}
	return items, nil
}
