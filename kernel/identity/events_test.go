package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

func TestUserEventsPublishOne(t *testing.T) {
	ctx := context.Background()
	var tx pgx.Tx
	tests := []struct {
		name    string
		publish func(identity.UserEvents) error
		typ, id string
		wantErr error
	}{
		{"user invited", func(e identity.UserEvents) error {
			return e.Invited(ctx, tx, identity.UserInvited{UserID: "u-2", InvitedBy: "u-1", RoleID: "r-1"})
		}, identity.EventUserInvited, "u-2", nil},
		{"invite list uploaded", func(e identity.UserEvents) error {
			return e.InviteListUploaded(ctx, tx, identity.InviteListUploaded{ListID: "l-1", UploadedBy: "u-1", Rows: 12})
		}, identity.EventUserInviteListUpload, "l-1", nil},
		{"offboard started", func(e identity.UserEvents) error {
			return e.OffboardStarted(ctx, tx, identity.OffboardStarted{UserID: "u-2", StartedBy: "u-1", LastDay: "2026-10-30"})
		}, identity.EventUserOffboardStarted, "u-2", nil},
		{"role changed", func(e identity.UserEvents) error {
			return e.RoleChanged(ctx, tx, identity.RoleChanged{UserID: "u-2", RoleIDs: []string{"r-2"}, GateID: "g-1"})
		}, identity.EventUserRoleChanged, "u-2", nil},
		{"deactivated", func(e identity.UserEvents) error {
			return e.Deactivated(ctx, tx, identity.Deactivated{UserID: "u-2", GateID: "g-2"})
		}, identity.EventUserDeactivated, "u-2", nil},
		{"invite without inviter", func(e identity.UserEvents) error {
			return e.Invited(ctx, tx, identity.UserInvited{UserID: "u-2"})
		}, "", "", identity.ErrInvalidAdminChange},
		{"role change without gate", func(e identity.UserEvents) error {
			return e.RoleChanged(ctx, tx, identity.RoleChanged{UserID: "u-2"})
		}, "", "", identity.ErrInvalidAdminChange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := outboxfake.New()
			err := tt.publish(identity.UserEvents{Emitter: f})
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := f.Messages()
			if tt.wantErr != nil {
				if len(msgs) != 0 {
					t.Fatalf("published %d events on error", len(msgs))
				}
				return
			}
			if len(msgs) != 1 || msgs[0].Type != tt.typ || msgs[0].Subject.ID != tt.id {
				t.Fatalf("messages = %+v, want one %s on %s", msgs, tt.typ, tt.id)
			}
		})
	}
	err := identity.UserEvents{}.Deactivated(ctx, tx, identity.Deactivated{UserID: "u-2"})
	if !errors.Is(err, identity.ErrNoEmitter) {
		t.Fatalf("no emitter: err = %v", err)
	}
}

// Expire publishes one break_glass.ended per grant it expires, and none on a re-run.
func TestBreakGlassExpirePublishesEnded(t *testing.T) {
	tests := []struct {
		name    string
		advance time.Duration
		want    int
	}{
		{"still active", time.Hour, 0},
		{"ended", 3 * time.Hour, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), time.UTC)
			bg := identity.NewBreakGlass(clk, auditfake.New("grant_id", "starts_at", "ends_at", "user_id"))
			f := outboxfake.New()
			bg.SetEvents(f)
			g, err := bg.Open(ctx, nil, bgRequest(nil))
			if err != nil {
				t.Fatal(err)
			}
			clk.Advance(tt.advance)
			for i := 0; i < 2; i++ {
				if _, err := bg.Expire(ctx, nil); err != nil {
					t.Fatal(err)
				}
			}
			msgs := f.Messages()
			if len(msgs) != tt.want {
				t.Fatalf("published %d events, want %d", len(msgs), tt.want)
			}
			if tt.want == 1 && (msgs[0].Type != identity.EventBreakGlassEnded || msgs[0].Subject.ID != g.ID) {
				t.Fatalf("message = %+v", msgs[0])
			}
		})
	}
}
