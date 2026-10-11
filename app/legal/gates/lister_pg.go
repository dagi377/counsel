package gates

import (
	"context"

	"github.com/dagi377/counsel/kernel/gate"
)

// PGLister is Lister over the kernel's Postgres gate list (gate.PGList), so the inbox reads
// the gate tables through the module that owns them.
type PGLister struct {
	Gates gate.PGList
}

var _ Lister = PGLister{}

// NewPGLister returns a PGLister reading in transactions on db.
func NewPGLister(db TxRunner) PGLister {
	return PGLister{Gates: gate.PGList{DB: db}}
}

// List implements Lister.
func (l PGLister) List(ctx context.Context, f ListFilter) ([]gate.Gate, error) {
	return l.Gates.List(ctx, gate.ListFilter{After: f.After, Limit: f.Limit, States: f.States, Kinds: f.Kinds,
		NotPreparedBy: f.NotPreparedBy})
}
