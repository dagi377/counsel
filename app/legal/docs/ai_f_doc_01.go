package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureIngestDocument is the spec id of "ingest-document".
const FeatureIngestDocument = "F-DOC-01"

// Gate kinds F-DOC-01 opens.
const (
	GateFilingPrivilege = "docs.filing_privilege" // G-D1: filing-queue review
	GateCalConfirm      = "cal.confirm"           // G7: confirm an extracted deadline
)

// Ingest item types.
const (
	IngestFiling   = "filing"
	IngestDeadline = "deadline"
)

// ingestQuery is what passages are ranked against. It is fixed feature text and never
// carries client content.
const ingestQuery = "document title, document type, court file number, privileged, " +
	"solicitor-client, without prejudice, deadline, due date, within days of service, hearing date"

// ingestSchema is the F-DOC-01 extraction output: at most one filing for the document and
// any deadlines it states, each citing the quoted passage it came from.
var ingestSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["type", "citation"],
        "additionalProperties": false,
        "properties": {
          "type": {"enum": ["filing", "deadline"]},
          "name": {"type": "string", "minLength": 1, "maxLength": 200},
          "kind": {"type": "string", "maxLength": 50},
          "privileged": {"type": "boolean"},
          "confidence_ppm": {"type": "integer", "minimum": 0, "maximum": 1000000},
          "title": {"type": "string", "minLength": 1, "maxLength": 200},
          "due_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "citation": ` + docCitationSchema + `
        }
      }
    }
  }
}`)

// IngestDocumentDef is the F-DOC-01 definition, for the featureset to add.
func IngestDocumentDef() feature.Def {
	return feature.Def{
		ID:      FeatureIngestDocument,
		Area:    "documents",
		Name:    "ingest-document",
		Source:  feature.SourceJob,
		Trigger: "event: upload, email-in, portal upload, scan",
		// embedding+extraction: passages are ranked by embedding, then one extraction call.
		Kind: ai.KindExtraction,
		Tools: []string{
			"get_matter", "list_documents", "read_document", "search_documents",
			"propose_deadline", "report_plan", "report_step", "submit_for_approval",
		},
		// docs.filing_privilege has no dedicated write tool: the run submits the filed
		// document. propose_deadline opens cal.confirm.
		GateKinds:    []string{GateFilingPrivilege, GateCalConfirm},
		Grant:        []string{"agent.ask", "deadlines.edit", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-doc-01.md",
		Fixture:      "testdata/ai/f-doc-01/ingest-document",
		OutputSchema: ingestSchema,
	}
}

// IngestFilingItem is where the ingested document goes. Name is model text: it goes to the
// sink only and is never logged.
type IngestFilingItem struct {
	Name          string
	Kind          string // one of Kinds()
	Privileged    bool
	ConfidencePPM int
	// Queued: privileged or under AutoFileReviewBelowPPM, so the document waits in the
	// filing queue for G-D1 instead of being filed.
	Queued   bool
	Citation agenttools.Citation
}

// IngestDeadlineItem is a deadline the document states, for G7. Title is model text.
type IngestDeadlineItem struct {
	Title    string
	DueDate  string // YYYY-MM-DD, America/Toronto
	Citation agenttools.Citation
}

// Ingested is one run's checked output.
type Ingested struct {
	Filing    *IngestFilingItem // nil: nothing to file
	Deadlines []IngestDeadlineItem
}

// IngestSink files the document or queues it for G-D1, and proposes the deadlines for G7,
// with the acting user as preparer. It is called once per run, only after the whole output
// checked out.
type IngestSink interface {
	Ingest(ctx context.Context, featureID string, in Ingested) error
}

// IngestPipeline is F-DOC-01's embedding+extraction pipeline.
type IngestPipeline struct {
	Loop     *run.Loop
	Embedder ai.Embedder
	Sink     IngestSink
}

// Run ranks the document passages by embedding, sends the best MaxRankedSources to one
// extraction call as quoted sources, and hands the cited filing and deadlines to the Sink.
// Output that fails the schema, cites a passage it was not given, names an unknown kind or
// an impossible date, or holds two filings writes nothing.
func (p IngestPipeline) Run(ctx context.Context, sources []agenttools.Quoted) (Ingested, error) {
	def := IngestDocumentDef()
	spec, err := def.Spec()
	if err != nil {
		return Ingested{}, err
	}
	best, err := docRank(ctx, p.Embedder, def.ID, ingestQuery, sources)
	if err != nil || len(best) == 0 {
		return Ingested{}, err
	}
	// rule: ARC-I-038
	// Passages reach the model only as quoted sources; Extract checks every item cites one.
	out, err := p.Loop.Extract(ctx, spec, best)
	if err != nil {
		return Ingested{}, err
	}
	in, err := decodeIngest(out)
	if err != nil {
		return Ingested{}, fmt.Errorf("%s: %w", def.ID, err)
	}
	if in.Filing == nil && len(in.Deadlines) == 0 {
		return Ingested{}, nil
	}
	if p.Sink == nil {
		return Ingested{}, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadItem)
	}
	// rule: AI-R-003
	// A queued filing opens G-D1 and every deadline opens G7; a person other than the
	// preparer decides them.
	if err := p.Sink.Ingest(ctx, def.ID, in); err != nil {
		return Ingested{}, fmt.Errorf("%s: ingest: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and counts are logged, never names, titles or passages.
	slog.InfoContext(ctx, "document ingested", "feature", def.ID,
		"queued", in.Filing != nil && in.Filing.Queued, "deadlines", len(in.Deadlines))
	return in, nil
}

// decodeIngest reads checked output. Errors carry positions only, never model text.
func decodeIngest(out json.RawMessage) (Ingested, error) {
	var doc struct {
		Items []struct {
			Type          string      `json:"type"`
			Name          string      `json:"name"`
			Kind          string      `json:"kind"`
			Privileged    bool        `json:"privileged"`
			ConfidencePPM *int        `json:"confidence_ppm"`
			Title         string      `json:"title"`
			DueDate       string      `json:"due_date"`
			Citation      docCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return Ingested{}, fmt.Errorf("decode output: %w", ErrBadItem)
	}
	var in Ingested
	for i, it := range doc.Items {
		cite := agenttools.Citation{VersionID: it.Citation.VersionID, Page: it.Citation.Page,
			FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine}
		switch it.Type {
		case IngestFiling:
			if in.Filing != nil || it.Name == "" || !knownKind(it.Kind) || it.ConfidencePPM == nil {
				return Ingested{}, fmt.Errorf("item %d: filing: %w", i, ErrBadItem)
			}
			conf := *it.ConfidencePPM
			in.Filing = &IngestFilingItem{
				Name: it.Name, Kind: it.Kind, Privileged: it.Privileged, ConfidencePPM: conf,
				Queued: it.Privileged || conf < AutoFileReviewBelowPPM, Citation: cite,
			}
		case IngestDeadline:
			if it.Title == "" {
				return Ingested{}, fmt.Errorf("item %d: deadline: %w", i, ErrBadItem)
			}
			if _, err := time.Parse(time.DateOnly, it.DueDate); err != nil {
				return Ingested{}, fmt.Errorf("item %d: due date: %w", i, ErrBadItem)
			}
			in.Deadlines = append(in.Deadlines, IngestDeadlineItem{Title: it.Title, DueDate: it.DueDate, Citation: cite})
		default:
			return Ingested{}, fmt.Errorf("item %d: type: %w", i, ErrBadItem)
		}
	}
	return in, nil
}
