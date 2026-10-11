package sync

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Mode is how a firm runs next to Clio.
type Mode string

const (
	// ModeAlongside keeps Clio the system of record: Counsel only reads from it.
	ModeAlongside Mode = "alongside"
	// ModeMigrated means Counsel is the system of record after a completed import.
	ModeMigrated Mode = "migrated"
)

var (
	// ErrSourceOfRecord is returned when an imported entity is edited in alongside mode.
	// Handlers answer 409 with this message.
	ErrSourceOfRecord = errors.New("source of record is Clio")
	// ErrAlongsideWrite is returned when a write request to Clio is attempted in alongside mode.
	ErrAlongsideWrite = errors.New("clio/sync: alongside mode never writes to Clio")
)

// webhookPath is the v4 webhook collection; managing webhooks is the one allowed write.
const webhookPath = "/api/v4/webhooks"

// CheckEditable reports whether an entity may be edited in Counsel. In alongside mode an
// imported entity is read-only and the edit must happen in Clio.
func CheckEditable(mode Mode, imported bool) error {
	// rule: MIG-040
	if mode == ModeAlongside && imported {
		return ErrSourceOfRecord
	}
	return nil
}

// ReadOnlyTransport wraps the transport of the Clio client in alongside mode. It lets
// GET through, and webhook management, and refuses every other request before
// it leaves the process.
type ReadOnlyTransport struct {
	Next http.RoundTripper // nil means http.DefaultTransport
}

// RoundTrip sends safe requests and refuses writes.
func (t ReadOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// rule: MIG-040
	if !readOnlyAllowed(r) {
		return nil, fmt.Errorf("%s %s: %w", r.Method, r.URL.Path, ErrAlongsideWrite)
	}
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(r)
}

func readOnlyAllowed(r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	p := r.URL.Path
	return p == webhookPath || strings.HasPrefix(p, webhookPath+".") || strings.HasPrefix(p, webhookPath+"/")
}

// Client returns an HTTP client for Clio in the given mode. In alongside mode its
// transport refuses writes; otherwise next is used as is.
func Client(mode Mode, next http.RoundTripper) *http.Client {
	if mode == ModeAlongside {
		return &http.Client{Transport: ReadOnlyTransport{Next: next}}
	}
	return &http.Client{Transport: next}
}
