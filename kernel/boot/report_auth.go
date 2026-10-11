package boot

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report"
)

var _ report.Authorizer = reportAuth{}

// ErrNoAccess means the report authorizer has no identity checker; every check fails.
var ErrNoAccess = errors.New("boot: no identity access checker")

// orgSubject is the subject a report capability is checked on: the zero Subject, which
// identity reads as the whole organisation, so no per-subject visibility applies. A report
// that filters rows by subject does that inside its own query.
var orgSubject = identity.Subject{}

// PrincipalResolver turns the user id a report run carries into the principal to check.
// It returns an error for an id it does not know.
type PrincipalResolver func(ctx context.Context, userID string) (identity.Principal, error)

// userPrincipal is the default resolver: the id is a human user.
func userPrincipal(_ context.Context, userID string) (identity.Principal, error) {
	return identity.Principal{Kind: registry.PrincipalUser, UserID: userID}, nil
}

// reportAuth is the report Authorizer over identity: a run is allowed only when the
// resolved principal holds the report's capability on the organisation subject. Every
// error, unknown user, empty capability or missing checker denies; it never allows on error.
type reportAuth struct {
	access  identity.Access
	resolve PrincipalResolver
}

// newReportAuth returns the identity-backed report Authorizer. resolve may be nil, in which
// case every id is checked as a human user.
func newReportAuth(access identity.Access, resolve PrincipalResolver) reportAuth {
	if resolve == nil {
		resolve = userPrincipal
	}
	return reportAuth{access: access, resolve: resolve}
}

// Can implements report.Authorizer.
func (a reportAuth) Can(ctx context.Context, userID, capability string) (bool, error) {
	if a.access == nil {
		return false, fmt.Errorf("report authorize: %w", ErrNoAccess)
	}
	if userID == "" {
		return false, fmt.Errorf("report authorize: %w", identity.ErrNoPrincipal)
	}
	if capability == "" {
		return false, fmt.Errorf("report authorize: %w", identity.ErrUnknownCapability)
	}
	p, err := a.resolve(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("report authorize: resolve principal: %w", err)
	}
	if principalUser(p) == "" {
		return false, fmt.Errorf("report authorize: %w", identity.ErrNoPrincipal)
	}
	ok, err := a.access.Can(ctx, p, capability, orgSubject)
	if err != nil {
		return false, fmt.Errorf("report authorize %q: %w", capability, err)
	}
	return ok, nil
}

// principalUser is the human a principal stands for: the user, or the user an agent run
// acts for. Empty means the principal names no one.
func principalUser(p identity.Principal) string {
	switch p.Kind {
	case registry.PrincipalUser, registry.PrincipalGuest:
		return p.UserID
	case registry.PrincipalAgent:
		if p.RunID == "" {
			return ""
		}
		return p.ActingFor
	}
	return ""
}

// reportAuthorizer is the report Authorizer Run installs: identity-backed when access is
// configured, else denyAll.
func reportAuthorizer(access identity.Access, resolve PrincipalResolver) report.Authorizer {
	if access == nil {
		return denyAll{}
	}
	return newReportAuth(access, resolve)
}
