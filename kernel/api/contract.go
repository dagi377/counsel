// CONTRACT: kernel/api types and interfaces. Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html, api/openapi.yaml.

package api

import "errors"

// ProblemContentType is the media type of every error body.
const ProblemContentType = "application/problem+json"

// Problem is an RFC 9457 problem+json body. Code is a stable machine code; Rule is the rule
// id the error enforces, when there is one. Detail never carries customer data or secrets.
type Problem struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
	Rule     string `json:"rule,omitempty"`
}

// Error lets a Problem travel as an error.
func (p *Problem) Error() string { return p.Code + ": " + p.Title }

// Cursor is the decoded position of a list page. After is the sort key of the last item
// returned; it is opaque to clients.
type Cursor struct {
	After string `json:"a"`
	Limit int    `json:"l"`
}

// Page is one page of a list response. Next is empty on the last page.
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
}

// CursorCodec turns cursors into opaque, tamper-evident strings and back.
type CursorCodec interface {
	// Encode returns the opaque form of c.
	Encode(c Cursor) (string, error)
	// Decode parses s; a tampered or malformed s returns ErrBadCursor.
	Decode(s string) (Cursor, error)
	// Clamp returns limit capped at the OpenAPI maximum; limit <= 0 gives the default.
	Clamp(limit int) int
}

// ErrBadCursor is returned for a malformed or tampered cursor (HTTP 400).
var ErrBadCursor = errors.New("api: bad cursor")
