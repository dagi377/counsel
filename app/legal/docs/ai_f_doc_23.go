package docs

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureDetectMatterContent is the spec id of "Detect matter content".
const FeatureDetectMatterContent = "F-DOC-23"

// ActionDetectMatterContent is the firm-library screen action that runs F-DOC-23 on request.
const ActionDetectMatterContent = "firm-library > Detect matter content"

// Matter content flags: what ties a firm-library passage to a matter.
const (
	FlagNamesClient = "names_client"
	FlagNamesParty  = "names_party"
)

// DetectMatterContentDef is the F-DOC-23 definition, for the featureset to add. It is
// read-only: it opens no gate and offers no write tool.
func DetectMatterContentDef() feature.Def {
	return feature.Def{
		ID:         FeatureDetectMatterContent,
		Area:       "documents",
		Name:       "Detect matter content",
		Source:     feature.SourceScreenAction,
		Trigger:    "on request: " + ActionDetectMatterContent,
		Actions:    []string{ActionDetectMatterContent},
		Kind:       ai.KindEmbedding,
		Tools:      []string{"get_matter", "list_documents", "read_document", "search_documents"},
		Grant:      []string{"agent.ask", "documents.view", "matters.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-doc-23.md",
		Fixture:    "testdata/ai/f-doc-23/detect-matter-content",
	}
}

// MatterContentFlag warns that a firm-library passage ties the file to a matter, and names
// the matter to move it to. It carries no client or party name.
type MatterContentFlag struct {
	Flag     string // FlagNamesClient or FlagNamesParty
	MatterID string
	Citation agenttools.Citation
}

// MatterContentFlags reads an F-DOC-23 run's output ({"items": [{flag, matter_id,
// citation}]}) into flag rows. An unknown flag, a missing matter or any other field fails the
// whole output with ErrBadItem; errors carry positions only, never model text.
func MatterContentFlags(out json.RawMessage) ([]MatterContentFlag, error) {
	var doc struct {
		Items []struct {
			Flag     string      `json:"flag"`
			MatterID string      `json:"matter_id"`
			Citation docCitation `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: decode output: %w", FeatureDetectMatterContent, ErrBadItem)
	}
	flags := make([]MatterContentFlag, 0, len(doc.Items))
	for i, it := range doc.Items {
		if it.Flag != FlagNamesClient && it.Flag != FlagNamesParty {
			return nil, fmt.Errorf("%s: item %d: flag: %w", FeatureDetectMatterContent, i, ErrBadItem)
		}
		if it.MatterID == "" {
			return nil, fmt.Errorf("%s: item %d: matter: %w", FeatureDetectMatterContent, i, ErrBadItem)
		}
		flags = append(flags, MatterContentFlag{
			Flag: it.Flag, MatterID: it.MatterID,
			Citation: agenttools.Citation{
				VersionID: it.Citation.VersionID, Page: it.Citation.Page,
				FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine,
			},
		})
	}
	return flags, nil
}
