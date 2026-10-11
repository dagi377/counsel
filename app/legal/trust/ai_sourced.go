package trust

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// The trust AI features (F-TRU-06, F-TRU-07, F-TRU-09) draft prose: a worksheet, a top-up
// request, a spot-audit pack. draft_document and draft_email carry their amounts inside
// body_markdown, where no JSON pointer reaches them, so the shared Provenance check cannot
// see them. SourcedDraft closes that gap for the trust features: every amount in the draft is
// written as integer cents and must be a figure a read tool returned in the same run.

// centsFigure is one amount as the trust prompts require it written: an integer, then
// "cents" ("125000 cents", "-4200 cents" for an overdrawn ledger).
var centsFigure = regexp.MustCompile(`(?i)(-?\d[\d,_]*(?:\.\d+)?)\s*cents\b`)

// unsourceableFigure is an amount written in a form the check cannot compare against the
// run's integer cents: a dollar sign or the word dollars.
var unsourceableFigure = regexp.MustCompile(`(?i)\$\s*-?\d|\d\s*dollars\b`)

// CheckSourcedCents returns agenttools.ErrUnsourcedValue unless every cents figure in text is
// one a read tool returned to runID (pv). It fails closed: no Provenance, no run id, a
// fractional or unparsable figure, or an amount in dollars is unsourced. Text with no amount
// passes. The error names no figure: a figure may be a client amount.
func CheckSourcedCents(pv *agenttools.Provenance, runID, text string) error {
	// rule: ARC-U-027
	// Fail closed: without the run's read values no amount can be shown to be sourced.
	if pv == nil || runID == "" {
		return fmt.Errorf("trust draft: no provenance: %w", agenttools.ErrUnsourcedValue)
	}
	// rule: ARC-U-027
	// A dollar figure cannot be matched to the integer cents the tools return.
	if unsourceableFigure.MatchString(text) {
		return fmt.Errorf("trust draft: amount not in integer cents: %w", agenttools.ErrUnsourcedValue)
	}
	ms := centsFigure.FindAllStringSubmatch(text, -1)
	if len(ms) == 0 {
		return nil
	}
	nums := make([]json.Number, 0, len(ms))
	for _, m := range ms {
		lit := strings.NewReplacer(",", "", "_", "").Replace(m[1])
		// rule: ARC-U-027
		// Money is integer cents: a fraction of a cent is never a figure a tool returned.
		if _, err := strconv.ParseInt(lit, 10, 64); err != nil {
			return fmt.Errorf("trust draft: amount not in integer cents: %w", agenttools.ErrUnsourcedValue)
		}
		nums = append(nums, json.Number(lit))
	}
	raw, err := json.Marshal(map[string][]json.Number{"cents": nums})
	if err != nil {
		return fmt.Errorf("trust draft: %w", err)
	}
	// rule: ARC-U-027
	if err := pv.Check(runID, []string{"/cents/*"}, raw); err != nil {
		return fmt.Errorf("trust draft: %w", err)
	}
	return nil
}

// draftText is the free text of a draft_document or draft_email input.
type draftText struct {
	Title        string `json:"title"`
	Subject      string `json:"subject"`
	BodyMarkdown string `json:"body_markdown"`
	Note         string `json:"note"`
}

// SourcedDraft wraps a draft write tool (draft_document, draft_email) so, for an agent
// principal, every cents figure in its title, subject and body must be one the run read
// (CheckSourcedCents). The check runs before the handler, so a draft with an invented amount
// is never stored and opens no gate. A user principal (the manual path) is not checked. It
// returns agenttools.ErrBadTool when t is not a write tool and ErrUnsourcedValue when pv is
// nil.
func SourcedDraft(pv *agenttools.Provenance, t registry.Tool) (registry.Tool, error) {
	if t.Kind != registry.ToolWrite || t.Handle == nil {
		return registry.Tool{}, fmt.Errorf("sourced draft %q: %w", t.Name, agenttools.ErrBadTool)
	}
	// rule: ARC-U-027
	if pv == nil {
		return registry.Tool{}, fmt.Errorf("sourced draft %q: no provenance: %w", t.Name, agenttools.ErrUnsourcedValue)
	}
	h := t.Handle
	name := t.Name
	t.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
		if p.Kind == registry.PrincipalAgent {
			var d draftText
			if err := json.Unmarshal(in, &d); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", name, agenttools.ErrInvalidInput)
			}
			text := strings.Join([]string{d.Title, d.Subject, d.BodyMarkdown, d.Note}, "\n")
			// rule: ARC-U-027
			if err := CheckSourcedCents(pv, p.RunID, text); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", name, err)
			}
		}
		return h(ctx, p, in)
	}
	return t, nil
}
