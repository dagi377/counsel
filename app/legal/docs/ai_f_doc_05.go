package docs

import (
	"embed"
	"encoding/json"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
)

// EventDocumentUploaded is the a2 event the docs module publishes when a document gets a new
// version. A new version of a document an open draft cites starts F-DOC-05.
const EventDocumentUploaded = "document.uploaded"

// ActionFlagStale is the precedent-library action that runs F-DOC-05 on request.
const ActionFlagStale = "precedent-library > Flag stale"

//go:embed prompts/f-doc-05.md
var promptFDOC05 embed.FS

// stalenessSchema is the F-DOC-05 final output: stale-citation flags, most urgent first. Each
// names the open draft and the document it cites, and cites the draft passage that relies on
// the superseded version.
var stalenessSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["rank", "draft_doc_id", "cited_doc_id", "reason", "citation"],
        "additionalProperties": false,
        "properties": {
          "rank": {"type": "integer", "minimum": 1},
          "draft_doc_id": {"type": "string", "minLength": 1, "maxLength": 100},
          "cited_doc_id": {"type": "string", "minLength": 1, "maxLength": 100},
          "reason": {"type": "string", "minLength": 1, "maxLength": 1000},
          "citation": {
            "type": "object",
            "required": ["version_id", "page", "from_line", "to_line"],
            "additionalProperties": false,
            "properties": {
              "version_id": {"type": "string", "minLength": 1},
              "page": {"type": "integer", "minimum": 1},
              "from_line": {"type": "integer", "minimum": 1},
              "to_line": {"type": "integer", "minimum": 1}
            }
          }
        }
      }
    }
  }
}`)

// CitationStalenessDef is F-DOC-05: when a document gets a new version, or on request from
// the precedent library, the agent reads the open drafts that cite it and returns
// stale-citation flags with citations. It is read-only: no write tool and no gate; a person
// decides what to do with each flag.
func CitationStalenessDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-05",
		Area:    "documents",
		Name:    "citation-staleness",
		Source:  feature.SourceJob,
		Trigger: "event: new version of a cited document",
		Actions: []string{ActionFlagStale},
		Kind:    ai.KindExtraction,
		// rule: ARC-I-035
		// Read tools only, so the run can change nothing and opens no gate.
		Tools:        []string{"get_matter", "list_documents", "read_document", "search_documents"},
		Grant:        []string{"matters.view", "documents.view"},
		Prompt:       promptFDOC05,
		PromptPath:   "prompts/f-doc-05.md",
		Fixture:      "testdata/ai/f-doc-05/citation-staleness",
		OutputSchema: stalenessSchema,
	}
}

// CitationStalenessBinding binds F-DOC-05 to the new-version event. Event runs act for the
// event's user, so it needs no scheduled acting user.
func CitationStalenessBinding() (trigger.Binding, error) {
	return trigger.Bind(CitationStalenessDef(), "", EventDocumentUploaded)
}
