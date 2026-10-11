package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// Renewal job constants (MIG-042, ARC-I-049).
const (
	// RenewJobKind is the River job kind. The job is periodic (RenewInterval) and unique, so two
	// workers running it at once send one PATCH per subscription.
	RenewJobKind = "subscription.renew"
	// RenewInterval is how often the job runs.
	RenewInterval = time.Hour
	// RenewWindow renews any subscription with expires_at <= now + RenewWindow.
	RenewWindow = 7 * 24 * time.Hour
	// AlertWindow raises an alert on a failed renewal when expires_at < now + AlertWindow.
	AlertWindow = 72 * time.Hour
	// EventRenewalFailed is emitted for every failed renewal.
	EventRenewalFailed = "source_subscription.renewal_failed"
)

// ErrNoExpiry is returned when the source answers a renewal without a later expiry.
var ErrNoExpiry = errors.New("clio/sync: renewal returned no later expiry")

// Subscription is one platform.source_subscription row.
type Subscription struct {
	ID            string
	Source        string
	RemoteID      string
	Events        []string
	ExpiresAt     time.Time
	LastRenewedAt *time.Time
	LastError     string
}

// SubscriptionStore reads and updates platform.source_subscription.
type SubscriptionStore interface {
	// Expiring returns the rows with expires_at <= before.
	Expiring(ctx context.Context, before time.Time) ([]Subscription, error)
	// MarkRenewed sets expires_at and last_renewed_at and clears last_error.
	MarkRenewed(ctx context.Context, id string, expiresAt, renewedAt time.Time) error
	// MarkFailed sets last_error and leaves expires_at unchanged.
	MarkFailed(ctx context.Context, id, lastError string) error
}

// SubscriptionRenewer extends a webhook subscription at the source (PATCH) and returns the
// new expiry.
type SubscriptionRenewer interface {
	Renew(ctx context.Context, remoteID string) (time.Time, error)
}

// RenewalFailure is the payload of EventRenewalFailed: ids and times only.
type RenewalFailure struct {
	SubscriptionID string    `json:"subscription_id"`
	Source         string    `json:"source"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// RenewalNotifier emits renewal events and alerts the firm admin and platform on-call.
type RenewalNotifier interface {
	// Emit records an event of type typ.
	Emit(ctx context.Context, typ string, f RenewalFailure) error
	// Alert notifies the firm admin and platform on-call that a subscription is about to lapse.
	Alert(ctx context.Context, f RenewalFailure) error
}

// RenewResult counts what one run did.
type RenewResult struct {
	Renewed int
	Failed  int
	Alerted int
}

// RenewJob renews source subscriptions that expire within RenewWindow.
type RenewJob struct {
	Store    SubscriptionStore
	Source   SubscriptionRenewer
	Notifier RenewalNotifier
	Clock    clock.Clock
}

// outcome is what happened to one subscription in a run.
type outcome int

const (
	renewed outcome = iota
	failed
	failedAlerted
)

// Run renews every expiring subscription once. A failed renewal is recorded on its row and
// retried on the next hourly run; it does not stop the others.
func (j *RenewJob) Run(ctx context.Context) (RenewResult, error) {
	var res RenewResult
	now := j.Clock.Now()
	// rule: MIG-042
	subs, err := j.Store.Expiring(ctx, now.Add(RenewWindow))
	if err != nil {
		return res, fmt.Errorf("renew subscriptions: list expiring: %w", err)
	}
	for _, s := range subs {
		o, err := j.renewOne(ctx, s, now)
		if err != nil {
			return res, err
		}
		switch o {
		case renewed:
			res.Renewed++
		case failed:
			res.Failed++
		case failedAlerted:
			res.Failed++
			res.Alerted++
		}
	}
	return res, nil
}

func (j *RenewJob) renewOne(ctx context.Context, s Subscription, now time.Time) (outcome, error) {
	// rule: MIG-041
	exp, err := j.Source.Renew(ctx, s.RemoteID)
	if err == nil && !exp.After(s.ExpiresAt) {
		err = ErrNoExpiry
	}
	if err == nil {
		// rule: MIG-042
		if err := j.Store.MarkRenewed(ctx, s.ID, exp, now); err != nil {
			return failed, fmt.Errorf("renew subscription %s: mark renewed: %w", s.ID, err)
		}
		return renewed, nil
	}
	slog.WarnContext(ctx, "source subscription renewal failed", "subscription", s.ID, "source", s.Source)
	// rule: MIG-042
	if err := j.Store.MarkFailed(ctx, s.ID, err.Error()); err != nil {
		return failed, fmt.Errorf("renew subscription %s: mark failed: %w", s.ID, err)
	}
	f := RenewalFailure{SubscriptionID: s.ID, Source: s.Source, ExpiresAt: s.ExpiresAt}
	// rule: ARC-I-049
	if err := j.Notifier.Emit(ctx, EventRenewalFailed, f); err != nil {
		return failed, fmt.Errorf("renew subscription %s: emit: %w", s.ID, err)
	}
	// rule: MIG-042
	if !s.ExpiresAt.Before(now.Add(AlertWindow)) {
		return failed, nil
	}
	if err := j.Notifier.Alert(ctx, f); err != nil {
		return failed, fmt.Errorf("renew subscription %s: alert: %w", s.ID, err)
	}
	return failedAlerted, nil
}
