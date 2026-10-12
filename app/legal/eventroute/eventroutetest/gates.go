package eventroutetest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
)

// Tx runs fn with a nil tx: the stores under test ignore it.
type Tx struct{}

// InTx implements eventroute.TxRunner.
func (Tx) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, nil)
}

// Opened is one gate a Gates fake opened.
type Opened struct {
	Kind     string
	Subject  gate.Subject
	Payload  json.RawMessage
	Preparer gate.Principal
}

// Gates records every gate it opens; Get returns it pending.
type Gates struct {
	Opened []Opened
}

// Open implements eventroute.Gates.
func (g *Gates) Open(_ context.Context, _ pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.Opened = append(g.Opened, Opened{Kind: kind, Subject: s, Payload: payload, Preparer: p})
	return gate.GateID(fmt.Sprintf("0192a000-0000-7000-8000-%012d", len(g.Opened))), nil
}

// Get implements eventroute.Gates.
func (g *Gates) Get(_ context.Context, id gate.GateID) (gate.Gate, error) {
	var n int
	if _, err := fmt.Sscanf(string(id)[24:], "%d", &n); err != nil || n < 1 || n > len(g.Opened) {
		return gate.Gate{}, gate.ErrNotFound
	}
	o := g.Opened[n-1]
	at := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	return gate.Gate{ID: id, Kind: o.Kind, Version: 1, Status: gate.StatusPending, Subject: o.Subject, Payload: o.Payload,
		Preparer: o.Preparer, CreatedAt: at, ExpiresAt: at.Add(72 * time.Hour)}, nil
}
