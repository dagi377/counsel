package roles

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

type walledStore struct {
	wallStore
	walled map[string]bool
}

func (s walledStore) Walled(_ context.Context, matterID string) (bool, error) {
	return s.walled[matterID], nil
}

type recEmitter struct{ types []string }

func (e *recEmitter) Emit(_ context.Context, _ pgx.Tx, _ outbox.Subject, typ string, _ any) error {
	e.types = append(e.types, typ)
	return nil
}

// rule: ARC-I-030
func TestWalledMatterAccessedPublishesOnce(t *testing.T) {
	store := walledStore{
		wallStore: wallStore{{"u-screened", "m-walled"}: true},
		walled:    map[string]bool{"m-walled": true},
	}
	tests := []struct {
		name, user, matter string
		want               int
	}{
		{"allowed access to a walled matter", "u-ok", "m-walled", 1},
		{"refused access to a walled matter", "u-screened", "m-walled", 1},
		{"open matter publishes nothing", "u-ok", "m-open", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			em := &recEmitter{}
			w := NewWallResolver(store, nil).WithEvents(em)
			if _, err := w.Visible(context.Background(), tc.user, tc.matter); err != nil {
				t.Fatal(err)
			}
			if len(em.types) != tc.want {
				t.Fatalf("events = %v, want %d", em.types, tc.want)
			}
			for _, typ := range em.types {
				if typ != EventWalledMatterAccessed {
					t.Fatalf("type = %s", typ)
				}
			}
		})
	}
}
