package conflict_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/conflict/conflictfake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
)

var wallNow = time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)

func newWalls(t *testing.T, walls ...conflict.Wall) *conflict.WallStore {
	t.Helper()
	s := conflict.NewWallStore(func() time.Time { return wallNow })
	for _, w := range walls {
		if err := s.AddWall(context.Background(), w); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestAddWallValidates(t *testing.T) {
	tests := []struct {
		name    string
		wall    conflict.Wall
		wantErr error
	}{
		{"complete", conflict.Wall{MatterID: "m1", UserID: "u1", CreatedBy: "u-law", Reason: "former adverse counsel"}, nil},
		{"no matter", conflict.Wall{UserID: "u1", CreatedBy: "u-law"}, conflict.ErrInvalidWall},
		{"no user", conflict.Wall{MatterID: "m1", CreatedBy: "u-law"}, conflict.ErrInvalidWall},
		{"no creator", conflict.Wall{MatterID: "m1", UserID: "u1"}, conflict.ErrInvalidWall},
		{"blank user", conflict.Wall{MatterID: "m1", UserID: "  ", CreatedBy: "u-law"}, conflict.ErrInvalidWall},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newWalls(t)
			err := s.AddWall(context.Background(), tc.wall)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil && tc.wall.Reason != "" && strings.Contains(err.Error(), tc.wall.Reason) {
				t.Fatalf("error leaks the wall reason: %v", err)
			}
		})
	}
}

func TestWallsAreAppendOnlyAndStamped(t *testing.T) {
	s := newWalls(t,
		conflict.Wall{MatterID: "m1", UserID: "u1", CreatedBy: "u-law"},
		conflict.Wall{MatterID: "m2", UserID: "u1", CreatedBy: "u-law"},
		conflict.Wall{MatterID: "m1", UserID: "u2", CreatedBy: "u-law"},
	)
	got, err := s.Walls(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UserID != "u1" || got[1].UserID != "u2" {
		t.Fatalf("walls on m1 = %+v", got)
	}
	for _, w := range got {
		if !w.CreatedAt.Equal(wallNow) {
			t.Fatalf("CreatedAt = %v, want %v", w.CreatedAt, wallNow)
		}
	}
}

func TestWallFeedsResolver(t *testing.T) {
	s := newWalls(t, conflict.Wall{MatterID: "m1", UserID: "u-walled", CreatedBy: "u-law"})
	tests := []struct {
		name, user, matter string
		wantWalled         bool
	}{
		{"walled user on walled matter", "u-walled", "m1", true},
		{"walled user on other matter", "u-walled", "m2", false},
		{"other user on walled matter", "u-other", "m1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			walled, err := s.Walled(context.Background(), tc.user, tc.matter)
			if err != nil {
				t.Fatal(err)
			}
			visible, err := s.Visible(context.Background(), tc.user, tc.matter)
			if err != nil {
				t.Fatal(err)
			}
			if walled != tc.wantWalled || visible == tc.wantWalled {
				t.Fatalf("walled=%v visible=%v, want walled=%v", walled, visible, tc.wantWalled)
			}
		})
	}
}

func TestWalledUserGetsNotFoundOnMatter(t *testing.T) {
	s := newWalls(t, conflict.Wall{MatterID: "m1", UserID: "u-walled", CreatedBy: "u-law"})
	f := conflictfake.New()
	ctx := context.Background()
	r, err := f.Check(ctx, conflict.Request{MatterID: "m1", Terms: []string{"acme"}, Preparer: conflict.Actor{UserID: "u-clerk"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Decide(ctx, r.ID, conflict.Actor{UserID: "u-law", IsLawyer: true}, true, "reviewed"); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	l := &matter.Lifecycle{Clock: clockfake.New(wallNow, loc), Conflicts: f, Walls: s}
	tests := []struct {
		name    string
		actor   string
		wantErr error
	}{
		{"walled user", "u-walled", matter.ErrNotFound},
		{"unwalled user", "u-law", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := l.Transition(ctx, matter.Matter{ID: "m1", Status: matter.Pending}, tc.actor, matter.Open)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAgentSearchMattersOmitsWalled(t *testing.T) {
	s := newWalls(t, conflict.Wall{MatterID: "m2", UserID: "u-walled", CreatedBy: "u-law"})
	all := []string{"m1", "m2", "m3"}
	tests := []struct {
		name  string
		actor conflict.Actor
		want  []string
	}{
		{"agent acting for walled user", conflict.Actor{UserID: "agent-1", IsAgent: true, ActingFor: "u-walled"}, []string{"m1", "m3"}},
		{"walled user directly", conflict.Actor{UserID: "u-walled"}, []string{"m1", "m3"}},
		{"agent acting for other user", conflict.Actor{UserID: "agent-1", IsAgent: true, ActingFor: "u-other"}, all},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := conflict.VisibleMatters(context.Background(), s, tc.actor, all)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("visible = %v, want %v", got, tc.want)
			}
		})
	}
}
