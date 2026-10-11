// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package matter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("matter: event body missing a required field")

// Kernel events published here that no feature file of this package declares.
const (
	EventClosed      = "matter.closed"       // a matter was closed
	EventTeamChanged = "matter.team_changed" // a matter's team changed
	EventUpdated     = "matter.updated"      // a matter's fields changed (field names only)
)

// LeadReadyToOpen is the body of EventLeadReadyToOpen: a lead is cleared and retained, ready to open as a matter.
type LeadReadyToOpen struct {
	LeadID string `json:"lead_id"`
	UserID string `json:"user_id"`
}

// PublishLeadReadyToOpen publishes EventLeadReadyToOpen in tx (nil: the emitter opens its own).
func PublishLeadReadyToOpen(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b LeadReadyToOpen) error {
	if b.LeadID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventLeadReadyToOpen, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "lead", ID: b.LeadID}, EventLeadReadyToOpen, b)
}

// CallTranscriptFiled is the body of EventCallTranscriptFiled: a call transcript was filed to a matter.
type CallTranscriptFiled struct {
	MatterID   string `json:"matter_id"`
	DocumentID string `json:"document_id"`
	UserID     string `json:"user_id"`
}

// PublishCallTranscriptFiled publishes EventCallTranscriptFiled in tx (nil: the emitter opens its own).
func PublishCallTranscriptFiled(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b CallTranscriptFiled) error {
	if b.MatterID == "" || b.DocumentID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventCallTranscriptFiled, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventCallTranscriptFiled, b)
}

// MatterClosed is the body of EventClosed: a matter was closed.
type MatterClosed struct {
	MatterID string `json:"matter_id"`
	ClosedOn string `json:"closed_on"`
	GateID   string `json:"gate_id"`
	UserID   string `json:"user_id"`
}

// PublishMatterClosed publishes EventClosed in tx (nil: the emitter opens its own).
func PublishMatterClosed(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b MatterClosed) error {
	if b.MatterID == "" || b.ClosedOn == "" || b.GateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventClosed, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventClosed, b)
}

// TeamChanged is the body of EventTeamChanged: a matter's team changed.
type TeamChanged struct {
	MatterID       string   `json:"matter_id"`
	AddedUserIDs   []string `json:"added_user_ids"`
	RemovedUserIDs []string `json:"removed_user_ids"`
	GateID         string   `json:"gate_id"`
	UserID         string   `json:"user_id"`
}

// PublishTeamChanged publishes EventTeamChanged in tx (nil: the emitter opens its own).
func PublishTeamChanged(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b TeamChanged) error {
	if b.MatterID == "" || b.GateID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventTeamChanged, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventTeamChanged, b)
}

// MatterUpdated is the body of EventUpdated: a matter's fields changed (field names only).
type MatterUpdated struct {
	MatterID      string   `json:"matter_id"`
	ChangedFields []string `json:"changed_fields"`
	Version       int64    `json:"version"`
	UserID        string   `json:"user_id"`
}

// PublishMatterUpdated publishes EventUpdated in tx (nil: the emitter opens its own).
func PublishMatterUpdated(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b MatterUpdated) error {
	if b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventUpdated, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventUpdated, b)
}

// WIPChanged is the body of EventWIPChanged: a matter's unbilled work in progress changed (no amount).
type WIPChanged struct {
	MatterID string `json:"matter_id"`
	UserID   string `json:"user_id"`
}

// PublishWIPChanged publishes EventWIPChanged in tx (nil: the emitter opens its own).
func PublishWIPChanged(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b WIPChanged) error {
	if b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventWIPChanged, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventWIPChanged, b)
}

// TrustBalanceChanged is the body of EventTrustBalanceChanged: a matter's trust balance changed (no amount).
type TrustBalanceChanged struct {
	MatterID string `json:"matter_id"`
	UserID   string `json:"user_id"`
}

// PublishTrustBalanceChanged publishes EventTrustBalanceChanged in tx (nil: the emitter opens its own).
func PublishTrustBalanceChanged(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b TrustBalanceChanged) error {
	if b.MatterID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventTrustBalanceChanged, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "matter", ID: b.MatterID}, EventTrustBalanceChanged, b)
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
