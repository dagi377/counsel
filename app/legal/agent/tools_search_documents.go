package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolSearchDocuments finds lines of a matter's documents that contain a query.
const ToolSearchDocuments = "search_documents"

// maxSearchPages caps how many pages of one version a search reads.
const maxSearchPages = 500

func init() {
	addHandler(Handler{Name: ToolSearchDocuments, Build: buildSearchDocuments})
}

// buildSearchDocuments reads Deps: CapabilityCheck, roles.Walls, MatterDocuments,
// DocumentText.
func buildSearchDocuments(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	docs, err := DepOf[MatterDocuments](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	text, err := DepOf[DocumentText](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	s := searchDocuments{walls: walls, docs: docs, text: text}
	return handlerTool(ToolSearchDocuments, Read, can, s.handle)
}

type searchDocuments struct {
	walls roles.Walls
	docs  MatterDocuments
	text  DocumentText
}

// searchHitOut names the document of the quote at the same index in "quotes".
type searchHitOut struct {
	DocID     string `json:"doc_id"`
	VersionID string `json:"version_id"`
}

// searchDocumentsOut returns every hit line as a quoted, cited envelope (ARC-I-038).
type searchDocumentsOut struct {
	Quotes     []agenttools.Quoted `json:"quotes"`
	Hits       []searchHitOut      `json:"hits"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

type searchHit struct {
	docID string
	v     kdocs.VersionID
	page  int
	line  int
	text  string
}

func (s searchDocuments) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		MatterID string `json:"matter_id"`
		Query    string `json:"query"`
		Cursor   string `json:"cursor"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchDocuments, ErrInvalidInput)
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	if q == "" {
		return registry.ToolResult{}, fmt.Errorf("%s: empty query: %w", ToolSearchDocuments, ErrInvalidInput)
	}
	offset, n, err := pageWindow(in.Cursor, in.Limit)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	// A walled matter is not found, not forbidden.
	if err := visibleTo(ctx, s.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	listed, err := s.docs.MatterDocuments(ctx, in.MatterID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: documents: %w", ToolSearchDocuments, err)
	}
	var hits []searchHit
	for _, ld := range listed {
		if ld.Document.MatterID != in.MatterID || ld.VersionID == "" {
			continue
		}
		found, err := s.searchVersion(ctx, ld.Document.ID, ld.VersionID, q)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
		}
		hits = append(hits, found...)
	}
	out := searchDocumentsOut{Quotes: []agenttools.Quoted{}, Hits: []searchHitOut{}, NextCursor: nextCursor(offset, n, len(hits))}
	var cites []registry.Citation
	if offset < len(hits) {
		for _, h := range hits[offset:min(offset+n, len(hits))] {
			c, err := s.text.Cite(ctx, h.v, h.page, h.line, h.line)
			if err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: cite: %w", ToolSearchDocuments, err)
			}
			src := registry.Citation{VersionID: string(c.VersionID), Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine}
			// rule: ARC-I-038
			// Document text goes back only as quoted untrusted data with its citation.
			out.Quotes = append(out.Quotes, agenttools.Quote(src, h.text))
			out.Hits = append(out.Hits, searchHitOut{DocID: h.docID, VersionID: string(h.v)})
			cites = append(cites, src)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSearchDocuments, err)
	}
	return registry.ToolResult{Output: b, Citations: cites}, nil
}

// searchVersion returns the lines of version v whose text contains q (lower case).
func (s searchDocuments) searchVersion(ctx context.Context, docID string, v kdocs.VersionID, q string) ([]searchHit, error) {
	var hits []searchHit
	for page := 1; page <= maxSearchPages; page++ {
		lines, err := s.text.Lines(ctx, v, page)
		if errors.Is(err, kdocs.ErrBadRange) {
			break // past the last page
		}
		if errors.Is(err, kdocs.ErrNotFound) {
			return nil, nil // no stored text for this version yet
		}
		if err != nil {
			return nil, fmt.Errorf("version text: %w", err)
		}
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l.Text), q) {
				hits = append(hits, searchHit{docID: docID, v: v, page: page, line: l.No, text: l.Text})
			}
		}
	}
	return hits, nil
}
