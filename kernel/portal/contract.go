// CONTRACT: kernel/portal. Frozen once on main; changes are additive only and need a
// docs/QUESTIONS.md entry. Bodies live in the T-035 subtasks (auth.go, pages.go).

package portal

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors.
var (
	// ErrPageNotFound is returned for a route no page is registered on (HTTP 404).
	ErrPageNotFound = errors.New("portal: page not found")
	// ErrDuplicatePage is returned when a page id or route is registered twice.
	ErrDuplicatePage = errors.New("portal: duplicate page")
	// ErrBadToken is returned for an unknown, used or expired sign-in token.
	ErrBadToken = errors.New("portal: bad sign-in token")
	// ErrNoSession is returned for an unknown or expired guest session.
	ErrNoSession = errors.New("portal: no session")
	// ErrNotGranted is returned when a guest asks for a subject its grant does not list.
	ErrNotGranted = errors.New("portal: subject not granted")
)

// Guest is an external person with portal access. Grants lists the subject ids the guest
// may see; nothing else is visible.
type Guest struct {
	ID     string
	Grants []string
}

// Session is a signed-in guest.
type Session struct {
	ID        string
	Guest     Guest
	ExpiresAt time.Time
}

// LoaderFunc loads the data a page shows for one granted subject.
type LoaderFunc func(ctx context.Context, g Guest, subject string) (any, error)

// PortalPage is a page the app registers through Registry.PortalPage.
type PortalPage struct {
	ID         string // module-prefixed id
	Route      string // path under the portal, e.g. "/docs"
	Capability string // capability the guest's grant must carry
	Load       LoaderFunc
}

// Portal is the portal shell: guest sign-in, sessions and the page registry.
type Portal interface {
	// RegisterPage adds a page. A duplicate id or route is an error.
	RegisterPage(p PortalPage) error
	// IssueToken creates a one-time magic-link token for a guest.
	IssueToken(ctx context.Context, g Guest, ttl time.Duration) (string, error)
	// SignIn exchanges a token for a session. A token works once.
	SignIn(ctx context.Context, token string) (Session, error)
	// Load runs the page on route for subject, if the session's guest is granted it.
	Load(ctx context.Context, sessionID, route, subject string) (any, error)
}
