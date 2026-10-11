package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	kdocs "github.com/dagi377/counsel/kernel/docs"
)

// Read tool names.
const (
	ToolSearchMatters = "search_matters"
	ToolGetMatter     = "get_matter"
	ToolReadDocument  = "read_document"
)

// Capabilities the read tools need.
const (
	CapMattersView   = "matters.view"
	CapDocumentsView = "documents.view"
)

// maxReadPages caps how many pages one read_document call returns.
const maxReadPages = 50

// CapabilityCheck reports whether a user holds a capability.
type CapabilityCheck func(ctx context.Context, userID, capability string) (bool, error)

// DocumentLookup finds the matter a filed document belongs to.
type DocumentLookup interface {
	Document(ctx context.Context, id string) (legaldocs.Document, error)
}

// DocumentText returns citable text of a stored document version.
type DocumentText interface {
	Lines(ctx context.Context, v kdocs.VersionID, page int) ([]kdocs.Line, error)
	Cite(ctx context.Context, v kdocs.VersionID, page, l0, l1 int) (kdocs.Citation, error)
}

// ReadDeps are what the read tools read through. Each comes from the owning module's
// query API.
type ReadDeps struct {
	Can       CapabilityCheck
	Matters   matter.Query
	Walls     roles.Walls
	Documents DocumentLookup
	Text      DocumentText
}

// ReadTools runs the Counsel read tools for an agent run. Walls of the acting-for user
// apply: a walled matter is omitted from searches and is not visible to any other tool.
type ReadTools struct {
	deps    ReadDeps
	specs   []ToolSpec
	schemas map[string]*jsonschema.Schema
}

const uuidDef = `"$defs":{"uuid":{"type":"string","format":"uuid"}}`

var readSpecs = []ToolSpec{
	{
		Name: ToolSearchMatters, Kind: Read, Capability: CapMattersView,
		InputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema",` + uuidDef + `,"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","maxLength":200},"status":{"enum":["open","pending","closed"]},"cursor":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50,"default":20}}}`),
	},
	{
		Name: ToolGetMatter, Kind: Read, Capability: CapMattersView,
		InputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema",` + uuidDef + `,"type":"object","additionalProperties":false,"required":["matter_id"],"properties":{"matter_id":{"$ref":"#/$defs/uuid"}}}`),
	},
	{
		Name: ToolReadDocument, Kind: Read, Capability: CapDocumentsView,
		InputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema",` + uuidDef + `,"type":"object","additionalProperties":false,"required":["doc_id"],"properties":{"doc_id":{"$ref":"#/$defs/uuid"},"version_id":{"$ref":"#/$defs/uuid"},"page_from":{"type":"integer","minimum":1},"page_to":{"type":"integer","minimum":1}}}`),
	},
}

// NewReadTools returns the read tools over deps.
func NewReadTools(deps ReadDeps) (*ReadTools, error) {
	t := &ReadTools{deps: deps, specs: readSpecs, schemas: map[string]*jsonschema.Schema{}}
	for _, s := range readSpecs {
		c := jsonschema.NewCompiler()
		url := "tool://" + s.Name + "/input.json"
		if err := c.AddResource(url, bytes.NewReader(s.InputSchema)); err != nil {
			return nil, fmt.Errorf("read tool %s schema: %w", s.Name, err)
		}
		sch, err := c.Compile(url)
		if err != nil {
			return nil, fmt.Errorf("read tool %s schema: %w", s.Name, err)
		}
		t.schemas[s.Name] = sch
	}
	return t, nil
}

// Specs returns the read tool specs.
func (t *ReadTools) Specs() []ToolSpec {
	return append([]ToolSpec(nil), t.specs...)
}

// Call runs one read tool.
func (t *ReadTools) Call(ctx context.Context, c Call) (Result, error) {
	var spec ToolSpec
	for _, s := range t.specs {
		if s.Name == c.Tool {
			spec = s
		}
	}
	if spec.Name == "" {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrUnknownTool)
	}
	var v any
	if err := json.Unmarshal(c.Input, &v); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	if err := t.schemas[spec.Name].Validate(v); err != nil {
		// Validation detail can echo client data; keep it out of the error.
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	ok, err := t.deps.Can(ctx, c.ActingFor, spec.Capability)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: capability: %w", c.Tool, err)
	}
	if !ok {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrForbidden)
	}
	switch spec.Name {
	case ToolSearchMatters:
		return t.searchMatters(ctx, c)
	case ToolGetMatter:
		return t.getMatter(ctx, c)
	default:
		return t.readDocument(ctx, c)
	}
}

type matterOut struct {
	MatterID  string `json:"matter_id"`
	Number    string `json:"number"`
	ShortName string `json:"short_name"`
	Status    string `json:"status"`
}

func toMatterOut(m matter.Matter) matterOut {
	return matterOut{MatterID: string(m.ID), Number: m.Number, ShortName: m.ShortName, Status: string(m.Status)}
}

func (t *ReadTools) visible(ctx context.Context, userID, matterID string) error {
	ok, err := t.deps.Walls.Visible(ctx, userID, matterID)
	if err != nil {
		return fmt.Errorf("walls: %w", err)
	}
	if !ok {
		return ErrNotVisible
	}
	return nil
}

func (t *ReadTools) searchMatters(ctx context.Context, c Call) (Result, error) {
	var in struct {
		Query  string `json:"query"`
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	all, err := t.deps.Matters.List(ctx, matter.Filter{Status: matter.Status(in.Status)})
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	out := []matterOut{}
	for _, m := range all {
		if len(out) == in.Limit {
			break
		}
		if q != "" && !strings.Contains(strings.ToLower(m.Number), q) && !strings.Contains(strings.ToLower(m.ShortName), q) {
			continue
		}
		// A walled matter is omitted, not reported as hidden.
		if err := t.visible(ctx, c.ActingFor, string(m.ID)); err != nil {
			if errors.Is(err, ErrNotVisible) {
				continue
			}
			return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
		}
		out = append(out, toMatterOut(m))
	}
	b, err := json.Marshal(map[string]any{"matters": out})
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	return Result{Output: b}, nil
}

func (t *ReadTools) getMatter(ctx context.Context, c Call) (Result, error) {
	var in struct {
		MatterID string `json:"matter_id"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	if err := t.visible(ctx, c.ActingFor, in.MatterID); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	m, err := t.deps.Matters.Get(ctx, matter.ID(in.MatterID))
	if errors.Is(err, matter.ErrNotFound) {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrNotVisible)
	}
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	b, err := json.Marshal(toMatterOut(m))
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	return Result{Output: b}, nil
}

type lineOut struct {
	No   int    `json:"no"`
	Text string `json:"text"`
}

type pageOut struct {
	Page  int       `json:"page"`
	Lines []lineOut `json:"lines"`
}

// documentOut is document text returned as quoted, untrusted data, never as instructions.
type documentOut struct {
	Untrusted bool      `json:"untrusted"`
	DocID     string    `json:"doc_id"`
	VersionID string    `json:"version_id"`
	Pages     []pageOut `json:"pages"`
}

func (t *ReadTools) readDocument(ctx context.Context, c Call) (Result, error) {
	var in struct {
		DocID     string `json:"doc_id"`
		VersionID string `json:"version_id"`
		PageFrom  int    `json:"page_from"`
		PageTo    int    `json:"page_to"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	// The catalog does not name a latest version, so the caller passes the version it lists.
	if in.VersionID == "" {
		return Result{}, fmt.Errorf("call %s: version_id required: %w", c.Tool, ErrInvalidInput)
	}
	d, err := t.deps.Documents.Document(ctx, in.DocID)
	if errors.Is(err, legaldocs.ErrNotFound) {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrNotVisible)
	}
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	if err := t.visible(ctx, c.ActingFor, d.MatterID); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	from, to := max(in.PageFrom, 1), in.PageTo
	if to == 0 || to-from+1 > maxReadPages {
		to = from + maxReadPages - 1
	}
	if to < from {
		return Result{}, fmt.Errorf("call %s: page range: %w", c.Tool, ErrInvalidInput)
	}
	v := kdocs.VersionID(in.VersionID)
	out := documentOut{Untrusted: true, DocID: d.ID, VersionID: in.VersionID}
	var cites []Citation
	for page := from; page <= to; page++ {
		lines, err := t.deps.Text.Lines(ctx, v, page)
		if errors.Is(err, kdocs.ErrBadRange) && page > from {
			break // past the last page
		}
		if errors.Is(err, kdocs.ErrNotFound) || errors.Is(err, kdocs.ErrBadRange) {
			return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrNotVisible)
		}
		if err != nil {
			return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
		}
		if len(lines) == 0 {
			continue
		}
		p := pageOut{Page: page}
		for _, l := range lines {
			p.Lines = append(p.Lines, lineOut{No: l.No, Text: l.Text})
		}
		// Every quoted page carries the citation of the lines it quotes.
		ct, err := t.deps.Text.Cite(ctx, v, page, lines[0].No, lines[len(lines)-1].No)
		if err != nil {
			return Result{}, fmt.Errorf("call %s: cite: %w", c.Tool, err)
		}
		cites = append(cites, Citation{VersionID: string(ct.VersionID), Page: ct.Page, FromLine: ct.FromLine, ToLine: ct.ToLine})
		out.Pages = append(out.Pages, p)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	return Result{Output: b, Citations: cites}, nil
}
