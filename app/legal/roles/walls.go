package roles

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
)

// SubjectMatter is the subject kind of a matter. Its subject id is the matter id.
const SubjectMatter = matterkind.Subject

// ErrNoMatterLookup means a subject kind has no way to find its matter.
var ErrNoMatterLookup = errors.New("roles: no matter lookup for subject kind")

// WallStore reports whether a user is screened from a matter by an active ethical wall
// (matter.ethical_wall with status active, through ethical_wall_matter and
// ethical_wall_screened_user). The matter module owns those tables and implements it.
type WallStore interface {
	Screened(ctx context.Context, userID, matterID string) (bool, error)
}

// MatterOf returns the matter a subject belongs to, or "" when it belongs to none.
type MatterOf func(ctx context.Context, subjectID string) (string, error)

var (
	_ Walls                    = (*WallResolver)(nil)
	_ registry.SubjectResolver = subjectResolver{}
)

// WallResolver is the Walls service. Its Subjects method is the kernel SubjectResolver for
// every subject kind that lives under a matter (the matter itself, its documents, deadlines
// and work items). A walled user sees them as not visible, which the kernel maps to 404.
type WallResolver struct {
	store   WallStore
	matters map[string]MatterOf
	events  outbox.Emitter // nil publishes nothing (WithEvents)
}

// NewWallResolver returns a resolver over store. matters maps each subject kind other than
// SubjectMatter to the lookup that finds its matter.
func NewWallResolver(store WallStore, matters map[string]MatterOf) *WallResolver {
	m := make(map[string]MatterOf, len(matters))
	for k, f := range matters {
		m[k] = f
	}
	return &WallResolver{store: store, matters: m}
}

// Visible implements Walls: false when userID is screened from matterID.
func (w *WallResolver) Visible(ctx context.Context, userID, matterID string) (bool, error) {
	if matterID == "" {
		return true, nil
	}
	screened, err := w.store.Screened(ctx, userID, matterID)
	if err != nil {
		return false, fmt.Errorf("walls: screened: %w", err)
	}
	if err := w.publishAccess(ctx, userID, matterID, !screened); err != nil {
		return false, err
	}
	return !screened, nil
}

// Subjects returns the SubjectResolver to register for SubjectMatter and every kind in the
// matters map.
func (w *WallResolver) Subjects() registry.SubjectResolver { return subjectResolver{w} }

type subjectResolver struct{ w *WallResolver }

// Visible implements registry.SubjectResolver. An agent run is walled exactly as the user
// it acts for.
func (r subjectResolver) Visible(ctx context.Context, p registry.Principal, s registry.Subject) (bool, error) {
	w := r.w
	user := p.UserID
	if p.Kind == registry.PrincipalAgent {
		user = p.ActingFor
	}
	if user == "" {
		return false, nil
	}
	matterID := s.ID
	if s.Kind != SubjectMatter {
		of, ok := w.matters[s.Kind]
		if !ok {
			return false, fmt.Errorf("walls: subject kind %q: %w", s.Kind, ErrNoMatterLookup)
		}
		var err error
		if matterID, err = of(ctx, s.ID); err != nil {
			return false, fmt.Errorf("walls: matter of %q: %w", s.Kind, err)
		}
	}
	return w.Visible(ctx, user, matterID)
}
