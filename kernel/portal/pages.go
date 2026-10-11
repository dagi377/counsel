package portal

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// ErrInvalidPage is returned by RegisterPage for a page that fails the portalpage schema
// (spec/kernel/portalpage.schema.json) or has no loader.
var ErrInvalidPage = errors.New("portal: invalid page")

var (
	pageID    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	pageRoute = regexp.MustCompile(`^/[a-z0-9/_{}-]*$`)
)

var _ Portal = (*Shell)(nil)

// RegisterPage adds a page the app registered. The id and capability are module-prefixed,
// the route is a portal path, and the page has a loader. A duplicate id or route is an error.
func (s *Shell) RegisterPage(p PortalPage) error {
	if !pageID.MatchString(p.ID) {
		return fmt.Errorf("register page %q: id must be module.name: %w", p.ID, ErrInvalidPage)
	}
	if !pageRoute.MatchString(p.Route) {
		return fmt.Errorf("register page %q: bad route %q: %w", p.ID, p.Route, ErrInvalidPage)
	}
	if !pageID.MatchString(p.Capability) {
		return fmt.Errorf("register page %q: capability must be module.name: %w", p.ID, ErrInvalidPage)
	}
	if p.Load == nil {
		return fmt.Errorf("register page %q: no loader: %w", p.ID, ErrInvalidPage)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pages[p.Route]; ok || s.pageIDs[p.ID] {
		return fmt.Errorf("register page %q on %q: %w", p.ID, p.Route, ErrDuplicatePage)
	}
	s.pages[p.Route] = p
	s.pageIDs[p.ID] = true
	return nil
}

// Load runs the page registered on route for subject. The session must be live and its
// guest's grant must list subject; the loader sees only that subject. A route with no
// registered page returns ErrPageNotFound (HTTP 404), checked after the session so an
// anonymous caller cannot probe which pages exist.
func (s *Shell) Load(ctx context.Context, sessionID, route, subject string) (any, error) {
	sess, err := s.Session(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load %q: %w", route, err)
	}
	s.mu.Lock()
	p, ok := s.pages[route]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("load %q: %w", route, ErrPageNotFound)
	}
	g, err := s.Authorize(ctx, sess.ID, subject)
	if err != nil {
		return nil, fmt.Errorf("load %q: %w", route, err)
	}
	v, err := p.Load(ctx, g, subject)
	if err != nil {
		return nil, fmt.Errorf("load %q: %w", route, err)
	}
	return v, nil
}
