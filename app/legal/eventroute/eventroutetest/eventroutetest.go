// Package eventroutetest has the access fake and request helper the event route tests share.
// No network, Postgres or Docker.
package eventroutetest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Test users: Walled is behind the ethical wall of every matter, NoCap lacks the capability
// on every matter, NoFirmCap lacks it firm-wide; any other user is allowed.
const (
	Walled    = "walled"
	NoCap     = "nocap"
	NoFirmCap = "nofirmcap"
	Lawyer    = "lawyer"
)

// Access answers by user: a walled user cannot see a matter (identity.ErrNotVisible), NoCap
// lacks the capability on a matter, NoFirmCap lacks it everywhere.
type Access struct{}

// Can implements eventroute.Access.
func (Access) Can(_ context.Context, p registry.Principal, _ string, s registry.Subject) (bool, error) {
	user := eventroute.User(p)
	if user == NoFirmCap {
		return false, nil
	}
	if s == (registry.Subject{}) {
		return true, nil
	}
	switch user {
	case Walled:
		return false, identity.ErrNotVisible
	case NoCap:
		return false, nil
	}
	return true, nil
}

// Route returns a ready eventroute.Route acting as p.
func Route(p registry.Principal) eventroute.Route {
	return eventroute.Route{Access: Access{}, Idem: idemfake.New(), Problems: &server.Problems{},
		Principal: func(*http.Request) (registry.Principal, bool) { return p, true }}
}

// User is a session principal for user.
func User(user string) registry.Principal {
	return registry.Principal{Kind: registry.PrincipalUser, UserID: user}
}

// Post serves a POST of body to path, with an Idempotency-Key, on the routes mounts.
func Post(mounts func(chi.Router), path, body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	mounts(r)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(idem.HeaderName, "key-00000001")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
