// CONTRACT: test/fakes/practiceapi types and interfaces. Frozen once on main; changes are
// additive only and need a docs/QUESTIONS.md entry.

// Package practiceapi is a fake practice-management REST API (v4-shaped JSON over HTTP) for
// import tests. It serves paged resources from testdata and records every request.
package practiceapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// APIPrefix is the path prefix of every served resource: GET {APIPrefix}<resource>.json.
const APIPrefix = "/api/v4/"

// Request is one recorded request.
type Request struct {
	Method string
	Path   string
	Query  string
}

// Paging is the meta.paging object of a list response.
type Paging struct {
	Next string `json:"next,omitempty"`
}

// Meta is the meta object of a list response.
type Meta struct {
	Paging Paging `json:"paging"`
	Count  int    `json:"records"`
}

// ListResponse is the body of GET {APIPrefix}<resource>.json.
type ListResponse struct {
	Data []json.RawMessage `json:"data"`
	Meta Meta              `json:"meta"`
}

// Server is the fake API.
type Server interface {
	// Load replaces the records of resource.
	Load(resource string, records []json.RawMessage)
	// Requests returns every request received, in order, including rejected ones.
	Requests() []Request
	// Handler serves the API; any method other than GET answers 405.
	Handler() http.Handler
}

// ErrUnknownResource is returned for a resource that was never loaded.
var ErrUnknownResource = errors.New("practiceapi: unknown resource")
