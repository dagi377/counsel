package agenttools

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrCitation means a tool result quotes a document without a valid citation in the kernel
// citation shape (version id, page, line range).
var ErrCitation = errors.New("agenttools: missing or invalid citation")

// ValidCitation checks one citation has the kernel shape: a document version id, a page from
// 1 and a line range FromLine..ToLine with 1 <= FromLine <= ToLine.
func ValidCitation(c Citation) error {
	switch {
	case c.VersionID == "":
		return fmt.Errorf("citation: version id required: %w", ErrCitation)
	case c.Page < 1:
		return fmt.Errorf("citation %s: page %d: %w", c.VersionID, c.Page, ErrCitation)
	case c.FromLine < 1 || c.ToLine < c.FromLine:
		return fmt.Errorf("citation %s: lines %d..%d: %w", c.VersionID, c.FromLine, c.ToLine, ErrCitation)
	}
	return nil
}

// CheckCitations checks a tool result: every citation it lists has the kernel shape, and every
// document passage its Output quotes (the "quotes" array of Quoted envelopes) has its source
// listed in Citations. A result that quotes nothing passes with no citations.
func CheckCitations(res ToolResult) error {
	for _, c := range res.Citations {
		if err := ValidCitation(c); err != nil {
			return fmt.Errorf("check citations: %w", err)
		}
	}
	var out struct {
		Quotes []Quoted `json:"quotes"`
	}
	if len(res.Output) > 0 {
		// Output that is not an object carries no quote envelope; only quotes need citations.
		if err := json.Unmarshal(res.Output, &out); err != nil {
			// A "quotes" field of the wrong shape fails closed.
			var typ *json.UnmarshalTypeError
			if !errors.As(err, &typ) || typ.Field != "" {
				return fmt.Errorf("check citations: output: %w", ErrCitation)
			}
		}
	}
	for i, q := range out.Quotes {
		if err := ValidCitation(q.Source); err != nil {
			return fmt.Errorf("check citations: quote %d: %w", i, err)
		}
		if !slices.Contains(res.Citations, q.Source) {
			return fmt.Errorf("check citations: quote %d source %s not cited: %w", i, q.Source.VersionID, ErrCitation)
		}
	}
	return nil
}
