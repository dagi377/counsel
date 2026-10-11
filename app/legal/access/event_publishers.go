// Minimal publishers for the kernel events this package's agent features bind to but no
// write path publishes yet (EVENTS-9, docs/QUESTIONS.md Q-EVENTS-9). The owning write path
// calls its publisher in the same transaction when it lands. Bodies carry ids, days, counts
// and codes only, plus user_id: the user an event run acts for (trigger.PayloadActor).

package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// ErrEventBody means an event body lacks a required id; nothing is published.
var ErrEventBody = errors.New("access: event body missing a required field")

// BreakGlassEnded is the body of EventBreakGlassEnded: a break-glass grant ended.
type BreakGlassEnded struct {
	GrantID string `json:"grant_id"`
	UserID  string `json:"user_id"`
}

// PublishBreakGlassEnded publishes EventBreakGlassEnded in tx (nil: the emitter opens its own).
func PublishBreakGlassEnded(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b BreakGlassEnded) error {
	if b.GrantID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventBreakGlassEnded, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "break_glass", ID: b.GrantID}, EventBreakGlassEnded, b)
}

// InviteListUploaded is the body of EventInviteListUploaded: an administrator uploaded an invite list.
type InviteListUploaded struct {
	UploadID string `json:"upload_id"`
	RowCount int64  `json:"row_count"`
	UserID   string `json:"user_id"`
}

// PublishInviteListUploaded publishes EventInviteListUploaded in tx (nil: the emitter opens its own).
func PublishInviteListUploaded(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b InviteListUploaded) error {
	if b.UploadID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventInviteListUploaded, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "invite_list", ID: b.UploadID}, EventInviteListUploaded, b)
}

// UserInvited is the body of EventUserInvited: an administrator invited a user.
type UserInvited struct {
	InviteID string `json:"invite_id"`
	UserID   string `json:"user_id"`
}

// PublishUserInvited publishes EventUserInvited in tx (nil: the emitter opens its own).
func PublishUserInvited(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b UserInvited) error {
	if b.InviteID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventUserInvited, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "invite", ID: b.InviteID}, EventUserInvited, b)
}

// OffboardStarted is the body of EventOffboardStarted: an administrator started offboarding a user.
type OffboardStarted struct {
	OffboardedUserID string `json:"offboarded_user_id"`
	UserID           string `json:"user_id"`
}

// PublishOffboardStarted publishes EventOffboardStarted in tx (nil: the emitter opens its own).
func PublishOffboardStarted(ctx context.Context, em outbox.Emitter, tx pgx.Tx, b OffboardStarted) error {
	if b.OffboardedUserID == "" || b.UserID == "" {
		return fmt.Errorf("publish %s: %w", EventOffboardStarted, ErrEventBody)
	}
	return publishEvent(ctx, em, tx, outbox.Subject{Kind: "user", ID: b.OffboardedUserID}, EventOffboardStarted, b)
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
