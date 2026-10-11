package roles

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

type wallStore map[[2]string]bool

func (s wallStore) Screened(_ context.Context, userID, matterID string) (bool, error) {
	if matterID == "broken" {
		return false, errors.New("db down")
	}
	return s[[2]string{userID, matterID}], nil
}

const (
	kindDocument = "docs.document"
	kindDeadline = "deadline.deadline"
	kindWorkItem = "work.work_item"
)

func newWalls() *WallResolver {
	parent := map[string]string{"d1": "m1", "dl1": "m1", "w1": "m1", "d2": "m2", "free": ""}
	of := func(_ context.Context, id string) (string, error) { return parent[id], nil }
	return NewWallResolver(
		wallStore{{"walled", "m1"}: true},
		map[string]MatterOf{kindDocument: of, kindDeadline: of, kindWorkItem: of},
	)
}

func TestWallsSubjectResolver(t *testing.T) {
	res := newWalls().Subjects()
	walled := registry.Principal{Kind: registry.PrincipalUser, UserID: "walled"}
	other := registry.Principal{Kind: registry.PrincipalUser, UserID: "other"}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "walled"}
	tests := []struct {
		name    string
		p       registry.Principal
		s       registry.Subject
		want    bool
		wantErr error
	}{
		{"walled user, matter", walled, registry.Subject{Kind: SubjectMatter, ID: "m1"}, false, nil},
		{"walled user, document", walled, registry.Subject{Kind: kindDocument, ID: "d1"}, false, nil},
		{"walled user, deadline", walled, registry.Subject{Kind: kindDeadline, ID: "dl1"}, false, nil},
		{"walled user, work item", walled, registry.Subject{Kind: kindWorkItem, ID: "w1"}, false, nil},
		{"walled user, other matter", walled, registry.Subject{Kind: SubjectMatter, ID: "m2"}, true, nil},
		{"walled user, other matter's document", walled, registry.Subject{Kind: kindDocument, ID: "d2"}, true, nil},
		{"walled user, item with no matter", walled, registry.Subject{Kind: kindWorkItem, ID: "free"}, true, nil},
		{"other user, matter", other, registry.Subject{Kind: SubjectMatter, ID: "m1"}, true, nil},
		{"agent acting for walled user", agent, registry.Subject{Kind: kindDocument, ID: "d1"}, false, nil},
		{"no user", registry.Principal{Kind: registry.PrincipalUser}, registry.Subject{Kind: SubjectMatter, ID: "m2"}, false, nil},
		{"unknown subject kind", other, registry.Subject{Kind: "x.y", ID: "1"}, false, ErrNoMatterLookup},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := res.Visible(context.Background(), tt.p, tt.s)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Visible err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Visible = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWallsVisibleStoreError(t *testing.T) {
	if _, err := newWalls().Visible(context.Background(), "walled", "broken"); err == nil {
		t.Fatal("Visible with failing store = nil error, want error")
	}
}

// A walled user's Can on the matter or anything under it fails with ErrNotVisible, which
// the kernel maps to 404.
func TestWallsKernelCanIsNotVisible(t *testing.T) {
	w := newWalls()
	r := identity.NewRoles()
	r.Capability(registry.Capability{ID: "matter.read"})
	r.Role(registry.Role{ID: "lawyer", Capabilities: []string{"matter.read"}})
	for _, k := range []string{SubjectMatter, kindDocument, kindDeadline, kindWorkItem} {
		r.SubjectKind(registry.SubjectKind{ID: k}, w.Subjects())
	}
	c := identity.NewChecker(r, identity.StaticAssignments{"walled": {"lawyer"}, "other": {"lawyer"}})
	tests := []struct {
		name    string
		user    string
		s       registry.Subject
		wantErr error
	}{
		{"walled, matter", "walled", registry.Subject{Kind: SubjectMatter, ID: "m1"}, identity.ErrNotVisible},
		{"walled, document", "walled", registry.Subject{Kind: kindDocument, ID: "d1"}, identity.ErrNotVisible},
		{"walled, deadline", "walled", registry.Subject{Kind: kindDeadline, ID: "dl1"}, identity.ErrNotVisible},
		{"walled, work item", "walled", registry.Subject{Kind: kindWorkItem, ID: "w1"}, identity.ErrNotVisible},
		{"not walled", "other", registry.Subject{Kind: kindDocument, ID: "d1"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := registry.Principal{Kind: registry.PrincipalUser, UserID: tt.user}
			ok, err := c.Can(context.Background(), p, "matter.read", tt.s)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Can err = %v, want %v", err, tt.wantErr)
			}
			if ok != (tt.wantErr == nil) {
				t.Fatalf("Can = %v, want %v", ok, tt.wantErr == nil)
			}
		})
	}
}
