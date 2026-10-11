package roles

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/kernel/outbox"
)

// EventWalledMatterAccessed is published on every wall check of a matter that an active
// ethical wall covers, allowed or refused. It starts F-CON-09 (wall breach review).
const EventWalledMatterAccessed = "roles.walled_matter_accessed"

// WalledStore reports whether an active ethical wall covers a matter. The matter module's
// wall store implements it next to Screened.
type WalledStore interface {
	Walled(ctx context.Context, matterID string) (bool, error)
}

// WalledMatterAccessed is the body of EventWalledMatterAccessed: ids and the outcome only.
type WalledMatterAccessed struct {
	MatterID string `json:"matter_id"`
	UserID   string `json:"user_id"`
	Allowed  bool   `json:"allowed"`
}

// WithEvents returns w publishing EventWalledMatterAccessed through em. The store must also
// implement WalledStore; otherwise nothing is published.
func (w *WallResolver) WithEvents(em outbox.Emitter) *WallResolver {
	w.events = em
	return w
}

// publishAccess emits EventWalledMatterAccessed when matterID is walled. It runs outside a
// transaction: the check is a read, and the access happened whether or not a later write
// commits.
func (w *WallResolver) publishAccess(ctx context.Context, userID, matterID string, allowed bool) error {
	ws, ok := w.store.(WalledStore)
	if w.events == nil || !ok {
		return nil
	}
	walled, err := ws.Walled(ctx, matterID)
	if err != nil {
		return fmt.Errorf("walls: walled: %w", err)
	}
	if !walled {
		return nil
	}
	// rule: ARC-I-030
	body := WalledMatterAccessed{MatterID: matterID, UserID: userID, Allowed: allowed}
	if err := w.events.Emit(ctx, nil, outbox.Subject{Kind: SubjectMatter, ID: matterID}, EventWalledMatterAccessed, body); err != nil {
		return fmt.Errorf("walls: publish access: %w", err)
	}
	return nil
}
