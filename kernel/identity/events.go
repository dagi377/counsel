package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

// User lifecycle events (spec/eng/a2-interfaces.json). Bodies carry ids, counts and times
// only, never names, addresses or free text.
const (
	EventUserInvited          = "user.invited"
	EventUserInviteListUpload = "user.invite_list_uploaded"
	EventUserOffboardStarted  = "user.offboard_started"
	EventUserRoleChanged      = "user.role_changed"
	EventUserDeactivated      = "user.deactivated"
	EventBreakGlassEnded      = "break_glass.ended"
	subjectUser               = "user"
	subjectBreakGlass         = "break_glass_grant"
	subjectInviteList         = "invite_list"
)

// ErrNoEmitter means a publisher has no emitter, so the event would be lost.
var ErrNoEmitter = errors.New("identity: no event emitter")

// UserEvents publishes the user lifecycle events in the caller's transaction, so the change
// and its event commit together. Write points without a home yet (invites, invite lists,
// offboarding, deactivation) call it directly (Q-EVENTS-8).
type UserEvents struct {
	Emitter outbox.Emitter
}

// UserInvited is the user.invited body.
type UserInvited struct {
	UserID    string `json:"user_id"`
	InvitedBy string `json:"invited_by"`
	RoleID    string `json:"role_id,omitempty"`
}

// InviteListUploaded is the user.invite_list_uploaded body.
type InviteListUploaded struct {
	ListID     string `json:"list_id"`
	UploadedBy string `json:"uploaded_by"`
	Rows       int    `json:"rows"`
}

// OffboardStarted is the user.offboard_started body.
type OffboardStarted struct {
	UserID    string `json:"user_id"`
	StartedBy string `json:"started_by"`
	LastDay   string `json:"last_day"` // YYYY-MM-DD, America/Toronto
	GateID    string `json:"gate_id,omitempty"`
}

// RoleChanged is the user.role_changed body (a2: user_id, role_ids[], gate_id).
type RoleChanged struct {
	UserID  string   `json:"user_id"`
	RoleIDs []string `json:"role_ids"`
	GateID  string   `json:"gate_id"`
}

// Deactivated is the user.deactivated body (a2: user_id, gate_id?).
type Deactivated struct {
	UserID string `json:"user_id"`
	GateID string `json:"gate_id,omitempty"`
}

// BreakGlassEnded is the break_glass.ended body.
type BreakGlassEnded struct {
	GrantID     string    `json:"grant_id"`
	UserID      string    `json:"user_id"`
	SubjectKind string    `json:"subject_kind"`
	SubjectID   string    `json:"subject_id"`
	EndedAt     time.Time `json:"ended_at"`
}

// Invited publishes user.invited.
func (e UserEvents) Invited(ctx context.Context, tx pgx.Tx, b UserInvited) error {
	return e.emit(ctx, tx, subjectUser, b.UserID, EventUserInvited, b, b.UserID != "" && b.InvitedBy != "")
}

// InviteListUploaded publishes user.invite_list_uploaded.
func (e UserEvents) InviteListUploaded(ctx context.Context, tx pgx.Tx, b InviteListUploaded) error {
	return e.emit(ctx, tx, subjectInviteList, b.ListID, EventUserInviteListUpload, b, b.ListID != "" && b.UploadedBy != "" && b.Rows >= 0)
}

// OffboardStarted publishes user.offboard_started.
func (e UserEvents) OffboardStarted(ctx context.Context, tx pgx.Tx, b OffboardStarted) error {
	return e.emit(ctx, tx, subjectUser, b.UserID, EventUserOffboardStarted, b, b.UserID != "" && b.StartedBy != "" && b.LastDay != "")
}

// RoleChanged publishes user.role_changed.
func (e UserEvents) RoleChanged(ctx context.Context, tx pgx.Tx, b RoleChanged) error {
	if b.RoleIDs == nil {
		b.RoleIDs = []string{}
	}
	return e.emit(ctx, tx, subjectUser, b.UserID, EventUserRoleChanged, b, b.UserID != "" && b.GateID != "")
}

// Deactivated publishes user.deactivated.
func (e UserEvents) Deactivated(ctx context.Context, tx pgx.Tx, b Deactivated) error {
	return e.emit(ctx, tx, subjectUser, b.UserID, EventUserDeactivated, b, b.UserID != "")
}

func (e UserEvents) emit(ctx context.Context, tx pgx.Tx, kind, id, typ string, body any, ok bool) error {
	if !ok {
		return fmt.Errorf("publish %s: %w", typ, ErrInvalidAdminChange)
	}
	if e.Emitter == nil {
		return fmt.Errorf("publish %s: %w", typ, ErrNoEmitter)
	}
	if err := e.Emitter.Emit(ctx, tx, outbox.Subject{Kind: kind, ID: id}, typ, body); err != nil {
		return fmt.Errorf("publish %s: %w", typ, err)
	}
	return nil
}

// SetEvents makes Expire publish break_glass.ended for each grant it expires, in the same tx
// as the expiry. With no emitter set, Expire publishes nothing.
func (b *BreakGlass) SetEvents(em outbox.Emitter) { b.events = em }

func (b *BreakGlass) publishEnded(ctx context.Context, tx pgx.Tx, g BreakGlassGrant) error {
	if b.events == nil {
		return nil
	}
	body := BreakGlassEnded{GrantID: g.ID, UserID: string(g.UserID), SubjectKind: g.Subject.Kind,
		SubjectID: g.Subject.ID, EndedAt: g.EndsAt}
	if err := b.events.Emit(ctx, tx, outbox.Subject{Kind: subjectBreakGlass, ID: g.ID}, EventBreakGlassEnded, body); err != nil {
		return fmt.Errorf("publish %s %s: %w", EventBreakGlassEnded, g.ID, err)
	}
	return nil
}
