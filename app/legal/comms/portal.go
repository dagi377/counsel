package comms

import (
	"context"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/kernel/registry"
)

// CapPortalView lets a portal guest see the items shared on their own matters.
const CapPortalView = "comms.portal_view"

// Portal page ids and routes.
const (
	PageMatters = "comms.portal_matters"
	PageItems   = "comms.portal_items"

	RouteMatters = "/matters"
	RouteItems   = "/matters/{matter}"

	// ParamMatter is the route parameter naming the matter on RouteItems.
	ParamMatter = "matter"
	// ParamKind optionally narrows RouteItems to one ItemKind ("document" or "bill").
	ParamKind = "kind"
)

// GuestResolver maps a signed-in guest's user id to the guest and the matters it has
// active portal access to (comms.portal_access rows not revoked).
type GuestResolver func(ctx context.Context, guestUserID string) (Guest, error)

// PortalPages returns the client portal pages. Each loader serves only guest principals and
// only the guest's own matters; anything else is ErrNotFound, so other matters do not exist
// for the guest.
func PortalPages(p Portal, resolve GuestResolver) []registry.PortalPage {
	l := loaders{p: p, resolve: resolve}
	return []registry.PortalPage{
		{ID: PageMatters, Route: RouteMatters, Capability: CapPortalView, Load: l.matters},
		{ID: PageItems, Route: RouteItems, Capability: CapPortalView, Load: l.items},
	}
}

// RegisterPortal registers the portal capability and pages on r.
func RegisterPortal(r registry.Registry, p Portal, resolve GuestResolver) {
	r.Capability(registry.Capability{ID: CapPortalView, Description: "See documents and bills shared on own matters in the client portal"})
	for _, pg := range PortalPages(p, resolve) {
		r.PortalPage(pg)
	}
}

type loaders struct {
	p       Portal
	resolve GuestResolver
}

func (l loaders) guest(ctx context.Context, pr registry.Principal) (Guest, error) {
	if pr.Kind != registry.PrincipalGuest || pr.UserID == "" {
		return Guest{}, ErrNotFound
	}
	g, err := l.resolve(ctx, pr.UserID)
	if err != nil {
		return Guest{}, fmt.Errorf("resolve guest: %w", err)
	}
	return g, nil
}

func (l loaders) matters(ctx context.Context, pr registry.Principal, _ map[string]string) (any, error) {
	g, err := l.guest(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("portal matters: %w", err)
	}
	ms, err := l.p.Matters(ctx, g)
	if err != nil {
		return nil, fmt.Errorf("portal matters: %w", err)
	}
	return ms, nil
}

func (l loaders) items(ctx context.Context, pr registry.Principal, params map[string]string) (any, error) {
	g, err := l.guest(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("portal items: %w", err)
	}
	matter := params[ParamMatter]
	// Checked here as well as in Portal.Items, so a Portal that forgets the check still
	// never shows another matter.
	if matter == "" || !slices.Contains(g.MatterIDs, matter) {
		return nil, fmt.Errorf("portal items: %w", ErrNotFound)
	}
	kind := ItemKind(params[ParamKind])
	if kind != "" && kind != ItemDocument && kind != ItemBill {
		return nil, fmt.Errorf("portal items: kind: %w", ErrInvalid)
	}
	all, err := l.p.Items(ctx, g, matter)
	if err != nil {
		return nil, fmt.Errorf("portal items: %w", err)
	}
	out := make([]SharedItem, 0, len(all))
	for _, it := range all {
		if it.MatterID != matter || (it.Kind != ItemDocument && it.Kind != ItemBill) {
			continue
		}
		if kind == "" || it.Kind == kind {
			out = append(out, it)
		}
	}
	return out, nil
}
