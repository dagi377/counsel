package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/app/legal/agent/schemas"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
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

// readSpecs name the read tools; capability and input schema come from the a2 definitions.
var readSpecs = []ToolSpec{
	{Name: ToolSearchMatters, Kind: Read},
	{Name: ToolGetMatter, Kind: Read},
	{Name: ToolReadDocument, Kind: Read},
}

// a2Specs fills each spec's capability and input schema from its a2 definition in package
// schemas, the one kernel/agenttools registers, and compiles that schema. A spec whose kind
// differs from its definition fails.
func a2Specs(in []ToolSpec) ([]ToolSpec, map[string]*jsonschema.Schema, error) {
	out := make([]ToolSpec, 0, len(in))
	compiled := map[string]*jsonschema.Schema{}
	for _, s := range in {
		d, err := schemas.ByName(s.Name)
		if err != nil {
			return nil, nil, err
		}
		if string(d.Kind) != string(s.Kind) {
			return nil, nil, fmt.Errorf("tool %s: kind %s, a2 says %s", s.Name, s.Kind, d.Kind)
		}
		sch, err := d.Compile()
		if err != nil {
			return nil, nil, err
		}
		s.Capability = d.Capability
		s.InputSchema = append(json.RawMessage(nil), d.Input...)
		compiled[s.Name] = sch
		out = append(out, s)
	}
	return out, compiled, nil
}

// NewReadTools returns the read tools over deps.
func NewReadTools(deps ReadDeps) (*ReadTools, error) {
	specs, sch, err := a2Specs(readSpecs)
	if err != nil {
		return nil, fmt.Errorf("read tools: %w", err)
	}
	return &ReadTools{deps: deps, specs: specs, schemas: sch}, nil
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

// matterOut is one matter as the read tools return it. The short name is user-written text,
// so it travels as quoted untrusted data, never as instructions; it has no document source to
// cite. The number is firm-assigned and the status an enum, so neither is free text.
type matterOut struct {
	MatterID  string            `json:"matter_id"`
	Number    string            `json:"number"`
	ShortName agenttools.Quoted `json:"short_name"`
	Status    string            `json:"status"`
}

// MatterCitation cites a matter record. A matter has no document version, so the record id
// stands in for the version, at page 1, line 1.
func MatterCitation(id matter.ID) agenttools.Citation {
	return agenttools.Citation{VersionID: "matter:" + string(id), Page: 1, FromLine: 1, ToLine: 1}
}

func toMatterOut(m matter.Matter) matterOut {
	return matterOut{
		MatterID: string(m.ID), Number: m.Number, Status: string(m.Status),
		// rule: ARC-I-038
		ShortName: agenttools.Quote(MatterCitation(m.ID), m.ShortName),
	}
}

func matterCite(id matter.ID) Citation {
	c := MatterCitation(id)
	return Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine}
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

// searchMattersOut is one page of search_matters. NextCursor is empty on the last page.
type searchMattersOut struct {
	Matters    []matterOut `json:"matters"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// searchMatters finds the matters the acting user can see. An empty query lists them all, a
// page at a time, so a scheduled sweep can walk every visible matter by following next_cursor.
func (t *ReadTools) searchMatters(ctx context.Context, c Call) (Result, error) {
	var in struct {
		Query  string `json:"query"`
		Status string `json:"status"`
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	offset, n, err := pageWindow(in.Cursor, in.Limit)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	all, err := t.deps.Matters.List(ctx, matter.Filter{Status: matter.Status(in.Status)})
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	var match []matter.Matter
	for _, m := range all {
		if q != "" && !strings.Contains(strings.ToLower(m.Number), q) && !strings.Contains(strings.ToLower(m.ShortName), q) {
			continue
		}
		// rule: ARC-I-036
		// A walled matter is omitted, not reported as hidden, and never counts toward a page.
		if err := t.visible(ctx, c.ActingFor, string(m.ID)); err != nil {
			if errors.Is(err, ErrNotVisible) {
				continue
			}
			return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
		}
		match = append(match, m)
	}
	out := searchMattersOut{Matters: []matterOut{}, NextCursor: nextCursor(offset, n, len(match))}
	var cites []Citation
	for i := offset; i < min(offset+n, len(match)); i++ {
		out.Matters = append(out.Matters, toMatterOut(match[i]))
		cites = append(cites, matterCite(match[i].ID))
	}
	b, err := json.Marshal(out)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	return Result{Output: b, Citations: cites}, nil
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
	return Result{Output: b, Citations: []Citation{matterCite(m.ID)}}, nil
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
