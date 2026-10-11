package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/roles"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/registry"
)

// ErrMissingDep means a tool handler was built without a module query API it reads through.
var ErrMissingDep = errors.New("agent: handler dependency missing")

// Deps carries the module query APIs real tool handlers read through. Each handler takes the
// values it needs with DepOf, so a new tool adds a file instead of a field to a shared struct.
type Deps struct {
	values []any
}

// NewDeps returns Deps over values.
func NewDeps(values ...any) Deps {
	return Deps{values: slices.Clone(values)}
}

// DepOf returns the first value in d that implements T.
func DepOf[T any](d Deps) (T, error) {
	for _, v := range d.values {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("%T: %w", (*T)(nil), ErrMissingDep)
}

// Handler builds one a2 tool's real registry handler from Deps.
type Handler struct {
	Name  string
	Build func(d Deps) (registry.Tool, error)
}

// ErrDuplicateHandler means a handler set lists two handlers for one tool.
var ErrDuplicateHandler = errors.New("agent: duplicate tool handler")

// HandlerSet is an explicit set of tool handlers, keyed by tool name. It is built from a list
// at the call site; nothing registers itself from init(), so the set a caller gets is exactly
// the list it was built from.
type HandlerSet struct {
	byName map[string]Handler
}

// NewHandlerSet returns the set of hs. Two handlers for one tool, an empty name or a nil
// Build fail with ErrDuplicateHandler or ErrMissingDep.
func NewHandlerSet(hs ...Handler) (HandlerSet, error) {
	s := HandlerSet{byName: make(map[string]Handler, len(hs))}
	for _, h := range hs {
		if h.Name == "" || h.Build == nil {
			return HandlerSet{}, fmt.Errorf("handler %q: %w", h.Name, ErrMissingDep)
		}
		if _, dup := s.byName[h.Name]; dup {
			return HandlerSet{}, fmt.Errorf("handler %s: %w", h.Name, ErrDuplicateHandler)
		}
		s.byName[h.Name] = h
	}
	return s, nil
}

// For returns the handler of a tool, if the set has one.
func (s HandlerSet) For(name string) (Handler, bool) {
	h, ok := s.byName[name]
	return h, ok
}

// Names lists the tools in the set, sorted.
func (s HandlerSet) Names() []string {
	names := make([]string, 0, len(s.byName))
	for n := range s.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Handlers is the app's handler set: every a2 tool with a real handler, listed here. A new
// tool file adds its Handler to this list. Boot builds the registry from this set; nothing
// registers itself from init(). Each handler reads its module ports (BillBalances,
// MatterTrustAccounts, PublishedPacks, MatterTime, SubjectMatters, ProposedTasks,
// ConflictCheckParties and the rest) from the Deps boot passes to Build; a port boot has not
// wired yet makes that Build fail with ErrMissingDep.
func Handlers() HandlerSet {
	hs := []Handler{
		{Name: ToolListDocuments, Build: buildListDocuments},
		{Name: ToolListWorkItems, Build: buildListWorkItems},
		{Name: ToolSearchContacts, Build: buildSearchContacts},
		{Name: ToolSearchDocuments, Build: buildSearchDocuments},
		DraftDocumentHandler(),
		DraftEmailHandler(),
		DraftNoteHandler(),
		ListDeadlinesHandler(),
		ComputeDeadlineHandler(),
		ListUnbilledHandler(),
		GetBillHandler(),
		GetTrustBalanceHandler(),
		ProposeTimeEntriesHandler(),
		ProposeDeadlineHandler(),
		CreateTaskHandler(),
		ProposeConflictPartiesHandler(),
		SubmitForApprovalHandler(),
		DraftBillHandler(),
		ProposePaymentAllocationHandler(),
		SearchMattersHandler(),
		GetMatterHandler(),
		ReadDocumentHandler(),
		PrepareTrustWithdrawalHandler(),
	}
	hs = append(hs, ReportHandlers()...)
	s, err := NewHandlerSet(hs...)
	if err != nil {
		// The list above is static; a duplicate is a programming error caught by its test.
		panic(err)
	}
	return s
}

// HandlerFor returns the real handler of a tool in Handlers().
func HandlerFor(name string) (Handler, bool) {
	return Handlers().For(name)
}

// HandlerNames lists the tools in Handlers(), sorted.
func HandlerNames() []string {
	return Handlers().Names()
}

// actor is the user whose capabilities and walls apply: the acting-for user of an agent run,
// else the calling user.
func actor(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// handlerTool builds the registry tool for one a2 read or report tool: name, kind, capability
// and input schema come from its a2 definition. The Registry refuses an agent principal whose
// grant lacks the capability and input failing the schema; handle also refuses an actor who
// lacks the capability.
func handlerTool(name string, kind ToolKind, can CapabilityCheck, handle func(ctx context.Context, user string, in json.RawMessage) (registry.ToolResult, error)) (registry.Tool, error) {
	if can == nil {
		return registry.Tool{}, fmt.Errorf("tool %s: capability check: %w", name, ErrMissingDep)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: name, Kind: kind}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("tool %s: %w", name, err)
	}
	s := specs[0]
	return registry.Tool{
		Name: s.Name, Kind: registry.ToolKind(s.Kind), Capability: s.Capability, Input: s.InputSchema,
		Handle: func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			user := actor(p)
			// rule: ARC-I-036
			ok, err := can(ctx, user, s.Capability)
			if err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", name, err)
			}
			if !ok {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", name, ErrForbidden)
			}
			return handle(ctx, user, in)
		},
	}, nil
}

// pageWindow reads the cursor and limit of a list input: the cursor is the offset of the next
// item, the limit defaults to 20.
func pageWindow(cursor string, limit int) (offset, n int, err error) {
	n = limit
	if n == 0 {
		n = 20
	}
	if cursor == "" {
		return 0, n, nil
	}
	if _, err := fmt.Sscanf(cursor, "%d", &offset); err != nil || offset < 0 || fmt.Sprint(offset) != cursor {
		return 0, 0, fmt.Errorf("cursor: %w", ErrInvalidInput)
	}
	return offset, n, nil
}

// nextCursor is the cursor after a window of n items from offset, or "" at the end.
func nextCursor(offset, n, total int) string {
	if offset+n >= total {
		return ""
	}
	return fmt.Sprint(offset + n)
}

// ListedDocument is one filed document of a matter with the version an agent reads and cites.
type ListedDocument struct {
	Document  legaldocs.Document
	VersionID kdocs.VersionID // the current stored version
}

// MatterDocuments lists the filed documents of one matter, in filing order.
type MatterDocuments interface {
	MatterDocuments(ctx context.Context, matterID string) ([]ListedDocument, error)
}

// visibleTo returns ErrNotVisible when walls hide matterID from user.
func visibleTo(ctx context.Context, walls roles.Walls, user, matterID string) error {
	// rule: ARC-I-036
	ok, err := walls.Visible(ctx, user, matterID)
	if err != nil {
		return fmt.Errorf("walls: %w", err)
	}
	if !ok {
		return ErrNotVisible
	}
	return nil
}
