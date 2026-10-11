package deadlines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureLimitationAssessment is the spec id of "Draft limitation assessment".
const FeatureLimitationAssessment = "F-DL-02"

// ErrRetryQueued means a transient model fault ended the run; a retry is queued and nothing
// was written.
var ErrRetryQueued = errors.New("deadlines: model fault, retry queued")

// assessPrompt is the run's user text. It is fixed: intake facts travel only as quoted
// triggers.
const assessPrompt = "Draft the limitation assessment for the intake facts quoted below."

// assessSchema is the F-DL-02 final output: the assessment summary with its citations and
// the limitation deadlines it proposes, each citing its source.
var assessSchema = json.RawMessage(`{
  "type": "object",
  "required": ["summary", "citations", "items"],
  "additionalProperties": false,
  "properties": {
    "summary": {"type": "string", "minLength": 1, "maxLength": 4000},
    "citations": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/citation"}},
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
          "citation": {"$ref": "#/$defs/citation"}
        }
      }
    }
  },
  "$defs": {
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
}`)

// LimitationAssessmentDef is the F-DL-02 definition, for app/legal/boot to add to its
// feature.Set.
func LimitationAssessmentDef() feature.Def {
	return feature.Def{
		ID:      FeatureLimitationAssessment,
		Area:    "deadlines",
		Name:    "Draft limitation assessment",
		Source:  feature.SourceJob,
		Trigger: "event: intake created or claim facts updated",
		Actions: []string{
			"limitation-assessment > Draft assessment from intake",
			"limitation-assessment > Send limitation warning to non-client",
		},
		Kind: ai.KindToolUse,
		Tools: []string{
			"get_matter", "read_document", "list_deadlines", "compute_deadline",
			"propose_deadline", "report_plan", "report_step", "submit_for_approval",
		},
		// cal.second_check has no dedicated write tool: the run drafts and submits it.
		GateKinds:    []string{"cal.confirm", "cal.second_check"},
		Grant:        []string{"agent.ask", "deadlines.edit", "deadlines.view", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-dl-02.md",
		Fixture:      "testdata/ai/f-dl-02/draft-limitation-assessment",
		OutputSchema: assessSchema,
	}
}

// Assessment is a checked draft limitation assessment. Summary is model text: it goes back
// to the caller for the lawyer's sign-off and is never logged or stored by the pipeline.
type Assessment struct {
	Summary   string
	Citations []agenttools.Citation
	Deadlines []ProposedDeadline
	GateIDs   []string // gates the run's write tools opened
}

// AssessPipeline is F-DL-02's tool-use pipeline.
type AssessPipeline struct {
	Runner   run.Runner
	Proposer Proposer
}

// Run drives one tool-use run as p (an agent principal acting for the user who prepares)
// over the intake facts, quoted as triggers. Output that fails the schema, cites a source
// that neither the triggers nor the run's tool results carried, or carries an unusable date
// writes nothing; a transient model fault writes nothing and returns ErrRetryQueued.
func (a AssessPipeline) Run(ctx context.Context, p run.Principal, facts []agenttools.Quoted) (Assessment, error) {
	def := LimitationAssessmentDef()
	spec, err := def.Spec()
	if err != nil {
		return Assessment{}, err
	}
	triggers := make([]run.Trigger, len(facts))
	sources := make([]agenttools.Citation, 0, len(facts))
	for i, f := range facts {
		if err := agenttools.ValidCitation(f.Source); err != nil {
			return Assessment{}, fmt.Errorf("%s: fact %d: %w", def.ID, i, err)
		}
		// rule: ARC-I-038
		// Intake facts reach the model only as quoted trigger data.
		triggers[i] = run.Trigger{Source: f.Source, Text: f.Text}
		sources = append(sources, f.Source)
	}
	out, err := a.Runner.Run(ctx, spec, p, assessPrompt, triggers...)
	if err != nil {
		return Assessment{}, err
	}
	if out.RetryQueued {
		return Assessment{}, fmt.Errorf("%s: %w", def.ID, ErrRetryQueued)
	}
	sources = append(sources, out.Citations...)
	as, err := decodeAssessment(out.JSON, sources)
	if err != nil {
		return Assessment{}, fmt.Errorf("%s: %w", def.ID, err)
	}
	as.GateIDs = out.GateIDs
	if err := proposeAll(ctx, a.Proposer, def.ID, as.Deadlines); err != nil {
		return Assessment{}, err
	}
	return as, nil
}

// decodeAssessment reads the schema-checked output and checks every citation falls inside
// one of sources. Errors carry positions only, never model text.
func decodeAssessment(out json.RawMessage, sources []agenttools.Citation) (Assessment, error) {
	var doc struct {
		Summary   string            `json:"summary"`
		Citations []json.RawMessage `json:"citations"`
		Items     []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &doc); err != nil {
		return Assessment{}, fmt.Errorf("decode output: %w", run.ErrBadOutput)
	}
	as := Assessment{Summary: doc.Summary}
	for i, raw := range doc.Citations {
		var c citationJSON
		if err := json.Unmarshal(raw, &c); err != nil {
			return Assessment{}, fmt.Errorf("citation %d: %w", i, run.ErrUncitedAnswer)
		}
		cite, err := c.check(sources)
		if err != nil {
			return Assessment{}, fmt.Errorf("citation %d: %w", i, err)
		}
		as.Citations = append(as.Citations, cite)
	}
	// The items share F-DL-01's proposal shape and date checks.
	itemsDoc, err := json.Marshal(struct {
		Items []json.RawMessage `json:"items"`
	}{doc.Items})
	if err != nil {
		return Assessment{}, fmt.Errorf("items: %w", run.ErrBadOutput)
	}
	items, err := decodeProposals(itemsDoc)
	if err != nil {
		return Assessment{}, err
	}
	for i, it := range items {
		if _, err := (citationJSON{it.Citation.VersionID, it.Citation.Page, it.Citation.FromLine, it.Citation.ToLine}).check(sources); err != nil {
			return Assessment{}, fmt.Errorf("item %d: %w", i, err)
		}
	}
	as.Deadlines = items
	return as, nil
}

type citationJSON struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

// check returns c as a kernel citation when it has the kernel shape and falls inside one of
// sources: same version and page, lines within the source's range.
func (c citationJSON) check(sources []agenttools.Citation) (agenttools.Citation, error) {
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
	return agenttools.Citation{}, fmt.Errorf("%s p%d: %w", cite.VersionID, cite.Page, run.ErrUncitedAnswer)
}
