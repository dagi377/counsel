package docs

import (
	"embed"
	"encoding/json"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/ai"
)

// ActionPlaceSigningFields is the signature-request screen action that runs F-DOC-12.
const ActionPlaceSigningFields = "signature-request > Place signing fields"

//go:embed prompts/f-doc-12.md
var promptFDOC12 embed.FS

// signingFieldsSchema is the F-DOC-12 final output: the signing fields to place, in signing
// order. Each names the document, the signer's role, the field and whether it sits on a
// template anchor or a detected signature block, and cites the page and lines it goes on.
var signingFieldsSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["rank", "doc_id", "signer_role", "field", "placement", "citation"],
        "additionalProperties": false,
        "properties": {
          "rank": {"type": "integer", "minimum": 1},
          "doc_id": {"type": "string", "minLength": 1, "maxLength": 100},
          "signer_role": {"type": "string", "minLength": 1, "maxLength": 100},
          "field": {"enum": ["signature", "initials", "date"]},
          "placement": {"enum": ["template_anchor", "detected_block"]},
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

// PlaceSigningFieldsDef is F-DOC-12: on the signature-request screen the agent reads the
// document to be signed and proposes where each signer's fields go, from the template's
// anchors or, without them, from the signature blocks it detects. It is read-only: no write
// tool and no gate; the user places the fields. A screen action starts it, acting for the user
// who asked, so it binds to no event or schedule.
func PlaceSigningFieldsDef() feature.Def {
	return feature.Def{
		ID:      "F-DOC-12",
		Area:    "documents",
		Name:    "Place signing fields",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionPlaceSigningFields,
		Actions: []string{ActionPlaceSigningFields},
		Kind:    ai.KindExtraction,
		// rule: ARC-I-035
		// Read tools only, so the run can change nothing and opens no gate.
		Tools:        []string{"get_matter", "list_documents", "read_document", "search_documents"},
		Grant:        []string{"matters.view", "documents.view"},
		Prompt:       promptFDOC12,
		PromptPath:   "prompts/f-doc-12.md",
		Fixture:      "testdata/ai/f-doc-12/place-signing-fields",
		OutputSchema: signingFieldsSchema,
	}
}
