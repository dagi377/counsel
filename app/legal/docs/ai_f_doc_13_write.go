package docs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
)

// ActionAutoFile is the matter-documents screen action that runs F-DOC-13 on request.
const ActionAutoFile = "matter-documents > Auto-file"

// autoFileSourced are the citation numbers of each suggestion, which must be page and line
// numbers the run's read tools returned.
var autoFileSourced = []string{
	"/items/*/citation/page", "/items/*/citation/from_line", "/items/*/citation/to_line",
}

// AutoFileFromRun is the write path of an F-DOC-13 screen-action run. The run's final output
// becomes filing suggestions for the Sink and nothing else: no write tool runs and no gate
// opens. Every suggestion must cite page and line numbers the run's read tools returned
// (pv holds runID's values; else agenttools.ErrUnsourcedValue) and fall inside a citation the
// run's tool results carried (out.Citations; else run.ErrUncitedAnswer). Any failure hands
// the Sink nothing.
func AutoFileFromRun(ctx context.Context, sink FilingSink, pv *agenttools.Provenance, runID string, out run.Outcome) ([]FilingSuggestion, error) {
	def := AutoFileDef()
	// rule: AI-R-003
	// The run proposes only: a run that opened a gate is not an Auto-file result.
	if len(out.GateIDs) != 0 {
		return nil, fmt.Errorf("%s: run opened %d gates: %w", def.ID, len(out.GateIDs), ErrBadItem)
	}
	items, err := decodeFilings(out.JSON)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if pv == nil {
		return nil, fmt.Errorf("%s: no provenance: %w", def.ID, agenttools.ErrUnsourcedValue)
	}
	// rule: ARC-I-038
	// A citation's numbers come from what the run read, never from the model alone.
	if err := pv.Check(runID, autoFileSourced, out.JSON); err != nil {
		return nil, fmt.Errorf("%s: %w", def.ID, err)
	}
	for i, it := range items {
		if err := agenttools.ValidCitation(it.Citation); err != nil {
			return nil, fmt.Errorf("%s: item %d: %w: %w", def.ID, i, run.ErrUncitedAnswer, err)
		}
		if !citedIn(out.Citations, it.Citation) {
			return nil, fmt.Errorf("%s: item %d: %w", def.ID, i, run.ErrUncitedAnswer)
		}
	}
	if sink == nil {
		return nil, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadItem)
	}
	if err := sink.SuggestFiling(ctx, def.ID, items); err != nil {
		return nil, fmt.Errorf("%s: suggest: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never names or passages.
	slog.InfoContext(ctx, "auto-file suggested", "feature", def.ID, "items", len(items), "source", "screen-action")
	return items, nil
}

// citedIn reports whether c falls inside one of the run's citations: same version and page,
// lines within the returned range.
func citedIn(cites []agenttools.Citation, c agenttools.Citation) bool {
	for _, r := range cites {
		if r.VersionID == c.VersionID && r.Page == c.Page && c.FromLine >= r.FromLine && c.ToLine <= r.ToLine {
			return true
		}
	}
	return false
}
