package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolListDeadlines lists the stored deadlines of the matters the acting user can see.
const ToolListDeadlines = "list_deadlines"

// CapDeadlinesView is the capability list_deadlines and compute_deadline need.
const CapDeadlinesView = "deadlines.view"

// ListDeadlinesHandler is list_deadlines' handler, for the boot HandlerSet.
func ListDeadlinesHandler() Handler {
	return Handler{Name: ToolListDeadlines, Build: buildListDeadlines}
}

// buildListDeadlines reads Deps: CapabilityCheck, roles.Walls, deadline.Lister.
func buildListDeadlines(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
	}
	list, err := DepOf[deadline.Lister](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
	}
	l := listDeadlines{walls: walls, list: list}
	return handlerTool(ToolListDeadlines, Read, can, l.handle)
}

type listDeadlines struct {
	walls roles.Walls
	list  deadline.Lister
}

// deadlineOut is one stored deadline. It carries ids, rule references and dates only, no
// document or client text, so it needs no quoted envelope. Due is a YYYY-MM-DD date: an
// agent run's provenance records it, so a later write may carry it.
type deadlineOut struct {
	DeadlineID  string `json:"deadline_id"`
	MatterID    string `json:"matter_id"`
	PackID      string `json:"pack_id"`
	PackVersion string `json:"pack_version"`
	RuleID      string `json:"rule_id"`
	Trigger     string `json:"trigger_date"`
	Due         string `json:"due_date"`
	State       string `json:"state"`
	Confirmed   bool   `json:"confirmed"`
}

type listDeadlinesOut struct {
	Deadlines  []deadlineOut `json:"deadlines"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (l listDeadlines) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		MatterID string `json:"matter_id"`
		From     string `json:"from"`
		To       string `json:"to"`
		Cursor   string `json:"cursor"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDeadlines, ErrInvalidInput)
	}
	offset, n, err := pageWindow(in.Cursor, in.Limit)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
	}
	var f deadline.Filter
	if f.From, err = optionalDate(in.From); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: from: %w", ToolListDeadlines, err)
	}
	if f.To, err = optionalDate(in.To); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: to: %w", ToolListDeadlines, err)
	}
	if in.MatterID != "" {
		// rule: ARC-I-036
		// A named walled matter is refused as not found.
		if err := visibleTo(ctx, l.walls, user, in.MatterID); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
		}
		f.Subject = &deadline.Subject{Kind: roles.SubjectMatter, ID: in.MatterID}
	}
	all, err := l.list.List(ctx, f)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: deadlines: %w", ToolListDeadlines, err)
	}
	var match []deadline.Deadline
	for _, dl := range all {
		s := dl.Proposed.Subject
		// A deadline on anything but a matter cannot be checked against walls: fail closed.
		if s.Kind != roles.SubjectMatter {
			continue
		}
		// rule: ARC-I-036
		// Without a matter filter, deadlines of walled matters are omitted, not refused.
		err := visibleTo(ctx, l.walls, user, s.ID)
		if errors.Is(err, ErrNotVisible) {
			continue
		}
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
		}
		match = append(match, dl)
	}
	out := listDeadlinesOut{Deadlines: []deadlineOut{}, NextCursor: nextCursor(offset, n, len(match))}
	for i := offset; i < min(offset+n, len(match)); i++ {
		dl := match[i]
		out.Deadlines = append(out.Deadlines, deadlineOut{
			DeadlineID: string(dl.ID), MatterID: dl.Proposed.Subject.ID,
			PackID: dl.Proposed.PackID, PackVersion: dl.Proposed.PackVersion, RuleID: dl.Proposed.RuleID,
			Trigger: dl.Proposed.Trigger.String(), Due: dl.Proposed.Due.String(),
			State: string(dl.State), Confirmed: dl.State == deadline.StateConfirmed,
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDeadlines, err)
	}
	return registry.ToolResult{Output: b}, nil
}

// parseDate reads a YYYY-MM-DD civil date.
func parseDate(s string) (clock.Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return clock.Date{}, ErrInvalidInput
	}
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

// optionalDate reads an optional YYYY-MM-DD date; "" is nil.
func optionalDate(s string) (*clock.Date, error) {
	if s == "" {
		return nil, nil
	}
	d, err := parseDate(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
