package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dagi377/counsel/kernel/api"
)

// Require is Can for handlers: it returns nil when p holds capability on s, ErrNotVisible
// when the subject's resolver hides s, and ErrForbidden when s is visible but p lacks the
// capability. Visibility is decided first, so a hidden subject never reports forbidden.
func Require(ctx context.Context, a Access, p Principal, capability string, s Subject) error {
	ok, err := a.Can(ctx, p, capability, s)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("require %q: %w", capability, ErrForbidden)
	}
	return nil
}

// problems maps identity sentinel errors to HTTP problems. ErrNotVisible is 404, never 403,
// so a hidden subject's existence does not leak.
var problems = []struct {
	err    error
	status int
	code   string
}{
	{ErrNotVisible, http.StatusNotFound, "not_found"},
	{ErrForbidden, http.StatusForbidden, "forbidden"},
	{ErrNoPrincipal, http.StatusUnauthorized, "unauthenticated"},
}

// Problem maps an identity error to a problem+json body. It returns nil for any other error,
// including ErrUnknownCapability and ErrUnknownSubjectKind, which are programming errors.
func Problem(err error) *api.Problem {
	for _, p := range problems {
		if errors.Is(err, p.err) {
			return &api.Problem{Title: http.StatusText(p.status), Status: p.status, Code: p.code}
		}
	}
	return nil
}
