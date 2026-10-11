// CONTRACT: app/legal/clio types and interfaces. Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Sources: spec/eng (MIG-001..MIG-011), docs/research/clio.

// Package clio imports a firm's data from the Clio v4 REST API: a read-only source client,
// rate limiting, decimal money, transform into module APIs, document versions and a dry run.
package clio

import (
	"context"
	"encoding/json"
	"errors"
)

// Region selects the Clio data region; it fixes the API host (MIG-001).
type Region string

const (
	RegionCA Region = "ca"
	RegionUS Region = "us"
	RegionEU Region = "eu"
	RegionAU Region = "au"
)

// Config configures the source client. Token is a secret: never logged or put in errors.
type Config struct {
	Region Region
	Token  string
	Scopes []string // granted OAuth scopes; any write scope is rejected (MIG-002)
}

// Resource is a v4 collection name.
type Resource string

const (
	ResourceContacts   Resource = "contacts"
	ResourceMatters    Resource = "matters"
	ResourceActivities Resource = "activities"
	ResourceBills      Resource = "bills"
	ResourceDocuments  Resource = "documents"
)

// Page is one page of a collection. Next is the opaque token of the next page; empty at the end.
type Page struct {
	Records []json.RawMessage
	Next    string
}

// Source reads collections from the source system. It never writes.
type Source interface {
	// List returns the page of res that starts at pageToken ("" for the first page).
	List(ctx context.Context, res Resource, pageToken string) (Page, error)
}

// Limiter paces requests to the source (MIG-003: 45 requests per minute at peak).
type Limiter interface {
	// Wait blocks until a request may be sent or ctx ends.
	Wait(ctx context.Context) error
}

// Count is the dry-run tally for one resource.
type Count struct {
	Found     int `json:"found"`
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Errors    int `json:"errors"`
}

// DryRunReport is the result of a dry run: what an import would do, with no writes.
type DryRunReport struct {
	Resources map[Resource]Count `json:"resources"`
}

// Importer runs imports.
type Importer interface {
	// DryRun reads every resource and reports what an import would change, writing nothing.
	DryRun(ctx context.Context) (DryRunReport, error)
}

var (
	// ErrUnknownRegion is returned for a region outside the Region constants.
	ErrUnknownRegion = errors.New("clio: unknown region")
	// ErrWriteScope is returned when the token carries any write scope (MIG-002).
	ErrWriteScope = errors.New("clio: token has a write scope")
	// ErrRateLimited is returned when the source answers 429 and retries are exhausted.
	ErrRateLimited = errors.New("clio: rate limited")
	// ErrBadDecimal is returned when a money string is not a valid decimal (MIG-005).
	ErrBadDecimal = errors.New("clio: bad decimal money")
	// ErrBadPageToken is returned for an unknown page token.
	ErrBadPageToken = errors.New("clio: bad page token")
)
