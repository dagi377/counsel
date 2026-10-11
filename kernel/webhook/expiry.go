package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// EventEndpointExpiring is emitted before an outbound endpoint with an expiry lapses.
const EventEndpointExpiring = "webhook_endpoint.expiring"

// ExpiryNoticeLeads are how long before expires_at an expiring event is emitted, longest first.
//
// rule: ARC-I-049
var ExpiryNoticeLeads = []time.Duration{14 * 24 * time.Hour, 24 * time.Hour}

// ErrPastExpiry means a renewal asked for an expiry that is not in the future.
var ErrPastExpiry = errors.New("webhook: expires_at is not in the future")

// EndpointExpiry is the expiry state of one platform.webhook row. ExpiresAt is nil for an
// endpoint the tenant owns, which never expires.
type EndpointExpiry struct {
	ID        string
	ExpiresAt *time.Time
}

// ExpiringNotice is the payload of EventEndpointExpiring.
type ExpiringNotice struct {
	EndpointID string    `json:"endpoint_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	Lead       string    `json:"lead"` // "336h0m0s" or "24h0m0s"
}

// ExpiryStore reads and updates endpoint expiry.
type ExpiryStore interface {
	// Expiring returns endpoints with a non-null expires_at <= before.
	Expiring(ctx context.Context, before time.Time) ([]EndpointExpiry, error)
	// ClaimNotice records that the notice for (id, expiresAt, lead) is sent. first is false
	// when it was already sent, and the caller must not emit it again.
	ClaimNotice(ctx context.Context, id string, expiresAt time.Time, lead time.Duration) (first bool, err error)
	// SetExpiry sets expires_at; nil means the endpoint never expires.
	SetExpiry(ctx context.Context, id string, expiresAt *time.Time) error
}

// ExpiryEmitter emits an EventEndpointExpiring event.
type ExpiryEmitter func(ctx context.Context, typ string, n ExpiringNotice) error

// InitialExpiry is the expires_at of a new endpoint: nil for a tenant-owned endpoint, else
// now + ttl.
func InitialExpiry(tenantOwned bool, now time.Time, ttl time.Duration) *time.Time {
	// rule: ARC-I-049
	if tenantOwned {
		return nil
	}
	t := now.Add(ttl)
	return &t
}

// NotifyExpiring emits one EventEndpointExpiring per endpoint and notice lead that is due at
// now, once. It returns the number of events emitted.
func NotifyExpiring(ctx context.Context, store ExpiryStore, emit ExpiryEmitter, now time.Time) (int, error) {
	eps, err := store.Expiring(ctx, now.Add(ExpiryNoticeLeads[0]))
	if err != nil {
		return 0, fmt.Errorf("notify expiring: list: %w", err)
	}
	sent := 0
	for _, ep := range eps {
		if ep.ExpiresAt == nil || !ep.ExpiresAt.After(now) {
			continue
		}
		lead, ok := dueLead(*ep.ExpiresAt, now)
		if !ok {
			continue
		}
		// rule: ARC-I-049
		first, err := store.ClaimNotice(ctx, ep.ID, *ep.ExpiresAt, lead)
		if err != nil {
			return sent, fmt.Errorf("notify expiring %s: claim: %w", ep.ID, err)
		}
		if !first {
			continue
		}
		n := ExpiringNotice{EndpointID: ep.ID, ExpiresAt: *ep.ExpiresAt, Lead: lead.String()}
		if err := emit(ctx, EventEndpointExpiring, n); err != nil {
			return sent, fmt.Errorf("notify expiring %s: emit: %w", ep.ID, err)
		}
		sent++
	}
	return sent, nil
}

// dueLead returns the shortest notice lead whose time (expiresAt - lead) has passed.
func dueLead(expiresAt, now time.Time) (time.Duration, bool) {
	for i := len(ExpiryNoticeLeads) - 1; i >= 0; i-- {
		if !now.Before(expiresAt.Add(-ExpiryNoticeLeads[i])) {
			return ExpiryNoticeLeads[i], true
		}
	}
	return 0, false
}

// RenewEndpoint applies PATCH /webhook-endpoints/{id} {expires_at}: a later expiry, or nil to
// never expire. Widening the subscribed events is a separate, gated change.
func RenewEndpoint(ctx context.Context, store ExpiryStore, id string, expiresAt *time.Time, now time.Time) error {
	// rule: ARC-I-049
	if expiresAt != nil && !expiresAt.After(now) {
		return fmt.Errorf("renew endpoint %s: %w", id, ErrPastExpiry)
	}
	if err := store.SetExpiry(ctx, id, expiresAt); err != nil {
		return fmt.Errorf("renew endpoint %s: %w", id, err)
	}
	return nil
}
