package access

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rule: ARC-I-030
// Each publisher writes exactly one outbox event of its type with ids-only keys; a body
// missing a required id publishes nothing (EVENTS-9).
func TestARC_I_030_AccessEventPublishers(t *testing.T) {
	tests := []struct {
		name    string
		publish func(context.Context, *outboxfake.Fake) error
		typ     string
		subject string
		keys    []string
		wantErr error
	}{
		{"break_glass.ended", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishBreakGlassEnded(ctx, em, nil, BreakGlassEnded{GrantID: "grant-id-1", UserID: "user-id-1"})
		}, EventBreakGlassEnded, "grant-id-1", []string{"grant_id", "user_id"}, nil},
		{"break_glass.ended without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishBreakGlassEnded(ctx, em, nil, BreakGlassEnded{})
		}, "", "", nil, ErrEventBody},
		{"user.invite_list_uploaded", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishInviteListUploaded(ctx, em, nil, InviteListUploaded{UploadID: "upload-id-1", RowCount: 1, UserID: "user-id-1"})
		}, EventInviteListUploaded, "upload-id-1", []string{"row_count", "upload_id", "user_id"}, nil},
		{"user.invite_list_uploaded without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishInviteListUploaded(ctx, em, nil, InviteListUploaded{})
		}, "", "", nil, ErrEventBody},
		{"user.invited", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishUserInvited(ctx, em, nil, UserInvited{InviteID: "invite-id-1", UserID: "user-id-1"})
		}, EventUserInvited, "invite-id-1", []string{"invite_id", "user_id"}, nil},
		{"user.invited without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishUserInvited(ctx, em, nil, UserInvited{})
		}, "", "", nil, ErrEventBody},
		{"user.offboard_started", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishOffboardStarted(ctx, em, nil, OffboardStarted{OffboardedUserID: "offboarded-user-id-1", UserID: "user-id-1"})
		}, EventOffboardStarted, "offboarded-user-id-1", []string{"offboarded_user_id", "user_id"}, nil},
		{"user.offboard_started without ids", func(ctx context.Context, em *outboxfake.Fake) error {
			return PublishOffboardStarted(ctx, em, nil, OffboardStarted{})
		}, "", "", nil, ErrEventBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := outboxfake.New()
			if err := tt.publish(context.Background(), em); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := em.Messages()
			if tt.wantErr != nil {
				if len(msgs) != 0 {
					t.Fatalf("events = %d, want 0", len(msgs))
				}
				return
			}
			if len(msgs) != 1 || msgs[0].Type != tt.typ || msgs[0].Subject.ID != tt.subject {
				t.Fatalf("events = %+v, want one %s on %s", msgs, tt.typ, tt.subject)
			}
			var body map[string]any
			if err := json.Unmarshal(msgs[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			var keys []string
			for k := range body {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, tt.keys) {
				t.Fatalf("payload keys = %v, want %v", keys, tt.keys)
			}
		})
	}
}
