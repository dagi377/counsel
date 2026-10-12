package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/outbox"
)

const (
	sqlNextRelease = `SELECT gate_id::text, kind, coalesce(matter_id::text, ''), coalesce(ref_id::text, ''), coalesce(recipient, '')
FROM platform.release WHERE status = 'queued'
ORDER BY approved_at, gate_id LIMIT 1 FOR UPDATE SKIP LOCKED`
	sqlMarkRelease = `UPDATE platform.release SET status = $2 WHERE gate_id = $1::uuid AND status = 'queued'`

	sqlNextDestruction = `SELECT gate_id::text, matter_id::text, retention_class
FROM platform.destruction_order WHERE status = 'scheduled'
ORDER BY approved_at, gate_id LIMIT 1 FOR UPDATE SKIP LOCKED`
	sqlMarkDestruction = `UPDATE platform.destruction_order SET status = $2 WHERE gate_id = $1::uuid AND status = 'scheduled'`
)

// PGReleases is the Postgres ReleaseStore.
type PGReleases struct{}

var _ ReleaseStore = PGReleases{}

// NextQueued implements ReleaseStore.
func (PGReleases) NextQueued(ctx context.Context, tx pgx.Tx) (Release, bool, error) {
	var r Release
	err := tx.QueryRow(ctx, sqlNextRelease).Scan(&r.GateID, &r.Kind, &r.MatterID, &r.RefID, &r.Recipient)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, false, nil
	}
	if err != nil {
		return Release{}, false, fmt.Errorf("next release: %w", err)
	}
	return r, true, nil
}

// Mark implements ReleaseStore.
func (PGReleases) Mark(ctx context.Context, tx pgx.Tx, gateID, status string) (bool, error) {
	tag, err := tx.Exec(ctx, sqlMarkRelease, gateID, status)
	if err != nil {
		return false, fmt.Errorf("mark release %s: %w", gateID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// PGDestructions is the Postgres DestructionStore.
type PGDestructions struct{}

var _ DestructionStore = PGDestructions{}

// NextScheduled implements DestructionStore.
func (PGDestructions) NextScheduled(ctx context.Context, tx pgx.Tx) (DestructionOrder, bool, error) {
	var o DestructionOrder
	err := tx.QueryRow(ctx, sqlNextDestruction).Scan(&o.GateID, &o.MatterID, &o.RetentionClass)
	if errors.Is(err, pgx.ErrNoRows) {
		return DestructionOrder{}, false, nil
	}
	if err != nil {
		return DestructionOrder{}, false, fmt.Errorf("next destruction order: %w", err)
	}
	return o, true, nil
}

// Mark implements DestructionStore.
func (PGDestructions) Mark(ctx context.Context, tx pgx.Tx, gateID, status string) (bool, error) {
	tag, err := tx.Exec(ctx, sqlMarkDestruction, gateID, status)
	if err != nil {
		return false, fmt.Errorf("mark destruction order %s: %w", gateID, err)
	}
	return tag.RowsAffected() == 1, nil
}

// EventSender is the production Sender: it publishes platform.release.<kind> (ids only) on the
// outbox in the release's tx; the channel's consumer reads the content from the gate payload.
type EventSender struct {
	Emitter outbox.Emitter
}

var _ Sender = EventSender{}

// Send implements Sender.
func (s EventSender) Send(ctx context.Context, tx pgx.Tx, r Release) error {
	if s.Emitter == nil {
		return fmt.Errorf("send release %s: %w", r.GateID, ErrRunnerDependency)
	}
	body := map[string]string{"gate_id": r.GateID, "kind": r.Kind}
	if r.MatterID != "" {
		body["matter_id"] = r.MatterID
	}
	if r.RefID != "" {
		body["ref_id"] = r.RefID
	}
	if err := s.Emitter.Emit(ctx, tx, outbox.Subject{Kind: SubjectRelease, ID: r.GateID}, SubjectRelease+"."+r.Kind, body); err != nil {
		return fmt.Errorf("send release %s: %w", r.GateID, err)
	}
	return nil
}
