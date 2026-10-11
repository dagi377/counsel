package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work"
)

// ToolListWorkItems lists work items, optionally for one matter, assignee or status.
const ToolListWorkItems = "list_work_items"

// CapWorkView is the capability list_work_items needs.
const CapWorkView = "work.view"

// WorkSubjectMatter is the subject kind of a matter-scoped work item.
const WorkSubjectMatter = "matter"

// WorkList lists work items (kernel/work Service.List).
type WorkList interface {
	List(ctx context.Context, f work.Filter) ([]work.Item, error)
}

// buildListWorkItems reads Deps: CapabilityCheck, roles.Walls, WorkList and the run's
// *agenttools.Provenance.
func buildListWorkItems(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
	}
	items, err := DepOf[WorkList](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
	}
	loc, err := time.LoadLocation(clock.DefaultZone)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: zone: %w", ToolListWorkItems, err)
	}
	l := listWorkItems{walls: walls, items: items, loc: loc}
	t, err := handlerTool(ToolListWorkItems, Read, can, l.handle)
	if err != nil {
		return registry.Tool{}, err
	}
	// rule: ARC-U-027
	// The dates this tool returns are values a later write in the run may carry.
	return sourcedRead(d, t)
}

type listWorkItems struct {
	walls roles.Walls
	items WorkList
	loc   *time.Location
}

// workItemOut is one listed item. The title is user-written text, so it travels as quoted
// untrusted data; it has no document source to cite.
type workItemOut struct {
	ItemID     string            `json:"item_id"`
	Key        string            `json:"key"`
	Type       string            `json:"type"`
	Title      agenttools.Quoted `json:"title"`
	StatusID   string            `json:"status_id"`
	MatterID   string            `json:"matter_id,omitempty"`
	AssigneeID string            `json:"assignee_id,omitempty"`
	Priority   int               `json:"priority"`
	DueAt      string            `json:"due_at,omitempty"`
	// DueOn is the due day in the business zone (YYYY-MM-DD): an agent run's provenance
	// records it, so a later write in the run may carry it. DueAt is an instant and is not.
	DueOn string `json:"due_on,omitempty"`
}

type listWorkItemsOut struct {
	Items      []workItemOut `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (l listWorkItems) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		MatterID   string `json:"matter_id"`
		AssigneeID string `json:"assignee_id"`
		Status     string `json:"status"`
		Cursor     string `json:"cursor"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListWorkItems, ErrInvalidInput)
	}
	offset, n, err := pageWindow(in.Cursor, in.Limit)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
	}
	f := work.Filter{AssigneeUserID: in.AssigneeID, StatusID: in.Status}
	if in.MatterID != "" {
		// A walled matter is not found, not forbidden.
		if err := visibleTo(ctx, l.walls, user, in.MatterID); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
		}
		f.Subject = work.Subject{Kind: WorkSubjectMatter, ID: in.MatterID}
	}
	all, err := l.items.List(ctx, f)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: list: %w", ToolListWorkItems, err)
	}
	var match []workItemOut
	for _, it := range all {
		var matterID string
		if it.Subject.Kind == WorkSubjectMatter {
			matterID = it.Subject.ID
			// rule: ARC-I-036
			// An item on a walled matter is omitted, not reported as hidden.
			if err := visibleTo(ctx, l.walls, user, matterID); err != nil {
				if errors.Is(err, ErrNotVisible) {
					continue
				}
				return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
			}
		}
		match = append(match, toWorkItemOut(it, matterID, l.loc))
	}
	out := listWorkItemsOut{Items: []workItemOut{}, NextCursor: nextCursor(offset, n, len(match))}
	if offset < len(match) {
		out.Items = append(out.Items, match[offset:min(offset+n, len(match))]...)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListWorkItems, err)
	}
	return registry.ToolResult{Output: b}, nil
}

func toWorkItemOut(it work.Item, matterID string, loc *time.Location) workItemOut {
	o := workItemOut{
		ItemID: string(it.ID), Key: it.Key, Type: it.Type, StatusID: it.StatusID,
		MatterID: matterID, AssigneeID: it.AssigneeUserID, Priority: it.Priority,
		// rule: ARC-I-038
		Title: agenttools.Quoted{Untrusted: true, Text: it.Title},
	}
	if o.Priority == 0 {
		o.Priority = 3
	}
	if it.DueAt != nil {
		o.DueAt = it.DueAt.Format(time.RFC3339)
		// rule: ARC-U-027
		o.DueOn = clock.DateOf(*it.DueAt, loc).String()
	}
	return o
}
