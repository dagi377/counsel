// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package conflict

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("conflict: event body missing a required field")

// CheckCompleted is the body of EventCheckCompleted: a conflict check's search ran.
type CheckCompleted struct {
	CheckID  string `json:"check_id"`
	MatterID string `json:"matter_id,omitempty"`
	LeadID   string `json:"lead_id,omitempty"`
	HitCount int64  `json:"hit_count"`
	UserID   string `json:"user_id"`
}

// PublishCheckCompleted publishes EventCheckCompleted in tx (nil: the emitter opens its own).
func PublishCheckCompleted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b CheckCompleted) error {
	if b.CheckID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventCheckCompleted, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "conflict_check", ID: b.CheckID}, EventCheckCompleted, b)
}

// ClearanceSubjectToConsent is the body of EventClearanceSubjectToConsent: a conflict clearance needs client consent.
type ClearanceSubjectToConsent struct {
	CheckID string `json:"check_id"`
	GateID  string `json:"gate_id"`
	UserID  string `json:"user_id"`
}

// PublishClearanceSubjectToConsent publishes EventClearanceSubjectToConsent in tx (nil: the emitter opens its own).
func PublishClearanceSubjectToConsent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b ClearanceSubjectToConsent) error {
	if b.CheckID == "" || b.GateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventClearanceSubjectToConsent, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "conflict_check", ID: b.CheckID}, EventClearanceSubjectToConsent, b)
}

// TrustFundsExpected is the body of EventTrustFundsExpected: trust funds are expected on a matter.
type TrustFundsExpected struct {
	RequestID string `json:"request_id"`
	MatterID  string `json:"matter_id"`
	UserID    string `json:"user_id"`
}

// PublishTrustFundsExpected publishes EventTrustFundsExpected in tx (nil: the emitter opens its own).
func PublishTrustFundsExpected(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b TrustFundsExpected) error {
	if b.RequestID == "" || b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventTrustFundsExpected, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "trust_request", ID: b.RequestID}, EventTrustFundsExpected, b)
}

// TrustRequestDrafted is the body of EventTrustRequestDrafted: a trust funds request was drafted.
type TrustRequestDrafted struct {
	RequestID string `json:"request_id"`
	MatterID  string `json:"matter_id"`
	UserID    string `json:"user_id"`
}

// PublishTrustRequestDrafted publishes EventTrustRequestDrafted in tx (nil: the emitter opens its own).
func PublishTrustRequestDrafted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b TrustRequestDrafted) error {
	if b.RequestID == "" || b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventTrustRequestDrafted, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "trust_request", ID: b.RequestID}, EventTrustRequestDrafted, b)
}

// publishEvent emits one event; a nil emitter publishes nothing.
func publishEvent(ctx context.Context, em outbox.Emitter, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if em == nil {
		return nil
	}
	// rule: ARC-I-030
	if err := em.Emit(ctx, tx, s, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}
