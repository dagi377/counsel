package docs

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureEmailAutoFile is the spec id of the email-filing "Auto-file".
const FeatureEmailAutoFile = "F-DOC-18"

// ActionEmailAutoFile is the email-filing screen action that runs F-DOC-18 on request.
const ActionEmailAutoFile = "email-filing > Auto-file"

// EmailFileAbovePPM is the confidence an email filing suggestion needs to be shown; the
// screen drops any suggestion below it.
const EmailFileAbovePPM = 900_000

// emailAutoFileSchema is the F-DOC-18 extraction output: where the email belongs, each
// suggestion citing the quoted lines it came from (run.ItemsField, run.CitationField).
var emailAutoFileSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "maxItems": 3,
      "items": {
        "type": "object",
        "required": ["matter_id", "confidence_ppm", "citation"],
        "additionalProperties": false,
        "properties": {
          "matter_id": {"type": "string", "minLength": 1, "maxLength": 100},
          "folder": {"type": "string", "maxLength": 200},
          "confidence_ppm": {"type": "integer", "minimum": 0, "maximum": 1000000},
          "citation": ` + docCitationSchema + `
        }
      }
    }
  }
}`)

// EmailAutoFileDef is the F-DOC-18 definition, for the featureset to add. It is read-only:
// it opens no gate and offers no write tool.
func EmailAutoFileDef() feature.Def {
	return feature.Def{
		ID:           FeatureEmailAutoFile,
		Area:         "documents",
		Name:         "Auto-file",
		Source:       feature.SourceScreenAction,
		Trigger:      "on request: " + ActionEmailAutoFile,
		Actions:      []string{ActionEmailAutoFile},
		Kind:         ai.KindExtraction,
		Tools:        []string{"get_matter", "list_documents", "read_document", "search_documents"},
		Grant:        []string{"agent.ask", "documents.view", "matters.view"},
		Prompt:       prompts,
		PromptPath:   "prompts/f-doc-18.md",
		Fixture:      "testdata/ai/f-doc-18/auto-file",
		OutputSchema: emailAutoFileSchema,
	}
}

// EmailFiling is where Auto-file suggests filing an email. Folder is model text: it goes to
// the screen only and is never logged.
type EmailFiling struct {
	MatterID      string
	Folder        string
	ConfidencePPM int
	Citation      agenttools.Citation
}

// EmailFilings reads an F-DOC-18 run's checked output and keeps only the suggestions at or
// above EmailFileAbovePPM. Output that does not decode is ErrBadItem; errors carry no model
// text.
func EmailFilings(out json.RawMessage) ([]EmailFiling, error) {
	var doc struct {
		Items []struct {
			MatterID      string      `json:"matter_id"`
			Folder        string      `json:"folder"`
			ConfidencePPM int         `json:"confidence_ppm"`
			Citation      docCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: decode output: %w", FeatureEmailAutoFile, ErrBadItem)
	}
	var items []EmailFiling
	for i, it := range doc.Items {
		if it.MatterID == "" {
			return nil, fmt.Errorf("%s: item %d: matter: %w", FeatureEmailAutoFile, i, ErrBadItem)
		}
		// rule: AI-R-003
		// Only a confident suggestion is shown; the user still files the email or ignores it.
		if it.ConfidencePPM < EmailFileAbovePPM {
			continue
		}
		items = append(items, EmailFiling{
			MatterID: it.MatterID, Folder: it.Folder, ConfidencePPM: it.ConfidencePPM,
			Citation: agenttools.Citation{
				VersionID: it.Citation.VersionID, Page: it.Citation.Page,
				FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine,
			},
		})
	}
	return items, nil
}
