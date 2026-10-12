package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	kaudit "github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/db"
)

// Job kinds of the runners that execute what the platform.* gate effects queue.
const (
	// KindReleaseSend sends every queued platform.release row.
	KindReleaseSend = "platform.release_send"
	// KindDestructionRun executes every scheduled platform.destruction_order row.
	KindDestructionRun = "platform.destruction_run"
)

// Audit actions the runners record, one per item outcome.
const (
	ActionReleaseSent        = "platform.release.sent"
	ActionReleaseFailed      = "platform.release.failed"
	ActionDestructionDone    = "platform.destruction.done"
	ActionDestructionBlocked = "platform.destruction.blocked"
)

// Audit subject kinds of the queued items.
const (
	SubjectRelease          = "platform.release"
	SubjectDestructionOrder = "platform.destruction_order"
)

// Sentinel errors of the runners.
var (
	// ErrRunnerDependency means a runner was built without a dependency it needs.
	ErrRunnerDependency = errors.New("platform runner: dependency missing")
	// ErrNoShredder means no file shredder is wired, so an eligible order stays scheduled.
	ErrNoShredder = errors.New("platform runner: no file shredder wired")
)

// runnerActor is the audit actor of both runners: the system, named by job kind.
const runnerActor = "system"

// maxPerPass bounds one pass so a large backlog cannot hold a worker forever; the next
// periodic run picks up the rest.
const maxPerPass = 500

// Release is one queued outbound item. The content stays in the approving gate's payload; the
// sender reads it by GateID.
type Release struct {
	GateID    string
	Kind      string
	MatterID  string
	RefID     string
	Recipient string
}

// ReleaseStore reads and moves platform.release rows inside the caller's tx.
type ReleaseStore interface {
	// NextQueued locks one queued release (skipping rows another worker holds); ok is false
	// when none is left.
	NextQueued(ctx context.Context, tx pgx.Tx) (r Release, ok bool, err error)
	// Mark moves a queued release to status; moved is false when it was no longer queued.
	Mark(ctx context.Context, tx pgx.Tx, gateID, status string) (moved bool, err error)
}

// Sender hands one approved release to its channel inside tx, so the hand-off commits with the
// release's move to sent.
type Sender interface {
	Send(ctx context.Context, tx pgx.Tx, r Release) error
}

// Auditor appends one audit event in the caller's tx (kernel audit.Log).
type Auditor interface {
	Append(ctx context.Context, tx pgx.Tx, e kaudit.Event) error
}

// ReleaseRunner sends queued releases. Each item is sent, moved to sent and audited in one
// tx, so a retry after a crash finds it sent and does nothing (ARC-I-012).
type ReleaseRunner struct {
	DB     db.DB
	Store  ReleaseStore
	Sender Sender
	Audit  Auditor
}

// Run sends every queued release and returns how many were sent. A release whose send fails is
// moved to failed and audited, and the pass goes on with the next one.
func (r ReleaseRunner) Run(ctx context.Context) (int, error) {
	if r.DB == nil || r.Store == nil || r.Sender == nil || r.Audit == nil {
		return 0, fmt.Errorf("%s: %w", KindReleaseSend, ErrRunnerDependency)
	}
	sent := 0
	for i := 0; i < maxPerPass; i++ {
		var (
			item    Release
			found   bool
			sendErr error
		)
		err := r.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			if item, found, err = r.Store.NextQueued(ctx, tx); err != nil || !found {
				return err
			}
			// rule: ARC-I-012
			// Only a row an approving gate queued is sent, and only while it is still queued.
			if sendErr = r.Sender.Send(ctx, tx, item); sendErr != nil {
				return sendErr
			}
			return r.finish(ctx, tx, item, "sent", ActionReleaseSent)
		})
		if !found && err == nil {
			return sent, nil
		}
		if sendErr != nil {
			slog.WarnContext(ctx, "release send failed", "gate_id", item.GateID, "kind", item.Kind, "err", sendErr)
			if err := r.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return r.finish(ctx, tx, item, "failed", ActionReleaseFailed)
			}); err != nil {
				return sent, fmt.Errorf("%s: mark failed %s: %w", KindReleaseSend, item.GateID, err)
			}
			continue
		}
		if err != nil {
			return sent, fmt.Errorf("%s: %w", KindReleaseSend, err)
		}
		sent++
	}
	return sent, nil
}

// finish moves the release and audits the move; a row no longer queued is left alone.
func (r ReleaseRunner) finish(ctx context.Context, tx pgx.Tx, item Release, status, action string) error {
	moved, err := r.Store.Mark(ctx, tx, item.GateID, status)
	if err != nil || !moved {
		return err
	}
	return r.Audit.Append(ctx, tx, kaudit.Event{ActorKind: runnerActor, ActorID: KindReleaseSend, Action: action,
		Subject: kaudit.Subject{Kind: SubjectRelease, ID: item.GateID}})
}

// DestructionOrder is one approved, scheduled file destruction.
type DestructionOrder struct {
	GateID         string
	MatterID       string
	RetentionClass string
}

// MatterRetention is the matter's retention state at the moment its order runs, read under a
// lock on the matter and its retention record.
type MatterRetention struct {
	Found            bool   // the matter has a retention record
	Status           string // matter.matter.status
	MatterHold       bool   // matter.matter.legal_hold
	RecordHold       bool   // matter.matter_closing.legal_hold
	DocumentHold     bool   // some document of the matter is on legal hold
	RelatedOpen      bool   // another matter of the same client is not closed
	RetentionClass   string // matter.matter_closing.retention_class
	DestroyAfter     *time.Time
	AlreadyDestroyed bool
	KeepSet          []string // record types kept regardless (trust, ID, conflict, closing letter, certificate)
}

// Reasons an order is blocked; each is part of the audit action's subject trail.
const (
	BlockNoRecord      = "no_retention_record"
	BlockHold          = "legal_hold"
	BlockNotClosed     = "matter_not_closed"
	BlockRelatedOpen   = "related_matter_open"
	BlockNotDue        = "retention_not_reached"
	BlockClassMismatch = "retention_class_changed"
)

// Blocked returns why an order may not run on day (America/Toronto), or "" when it may. A
// hold anywhere on the matter blocks the whole order: nothing under a hold is destroyed.
func Blocked(o DestructionOrder, m MatterRetention, day time.Time) string {
	switch {
	case !m.Found:
		return BlockNoRecord
	// A hold anywhere on the matter suspends destruction (spec/legal archive-retention).
	case m.MatterHold || m.RecordHold || m.DocumentHold:
		return BlockHold
	case m.Status != "closed" && m.Status != "archived":
		return BlockNotClosed
	case m.RelatedOpen:
		return BlockRelatedOpen
	case m.RetentionClass != o.RetentionClass:
		return BlockClassMismatch
	case m.DestroyAfter == nil || day.Before(*m.DestroyAfter):
		return BlockNotDue
	}
	return ""
}

// DestructionStore reads and moves destruction orders.
type DestructionStore interface {
	// NextScheduled locks one scheduled order; ok is false when none is left.
	NextScheduled(ctx context.Context, tx pgx.Tx) (o DestructionOrder, ok bool, err error)
	// Mark moves a scheduled order to status (done or blocked); moved is false otherwise.
	Mark(ctx context.Context, tx pgx.Tx, gateID, status string) (moved bool, err error)
}

// RetentionRecords is the matter module's retention record (ARC-I-041: only the matter module
// reads its tables).
type RetentionRecords interface {
	// Retention reads the matter's retention state, locking the matter and its record.
	Retention(ctx context.Context, tx pgx.Tx, matterID string) (MatterRetention, error)
	// RecordDestroyed stamps the retention record destroyed (never one on hold).
	RecordDestroyed(ctx context.Context, tx pgx.Tx, matterID, gateID string, at time.Time) error
}

// Shredder destroys a matter's file contents outside keepSet (crypto-shred), inside tx.
type Shredder interface {
	Shred(ctx context.Context, tx pgx.Tx, matterID string, keepSet []string) error
}

// DestructionRunner executes approved destruction orders. It re-checks every hold and the
// retention rules at run time, under a lock on the matter, so a hold placed after approval
// still stops the destruction.
type DestructionRunner struct {
	DB       db.DB
	Store    DestructionStore
	Records  RetentionRecords
	Shredder Shredder // nil: eligible orders stay scheduled and the pass fails with ErrNoShredder
	Audit    Auditor
	Now      func() time.Time
	Zone     *time.Location
}

// DestructionResult counts one pass.
type DestructionResult struct {
	Done, Blocked int
}

// Run executes every scheduled order. A blocked order moves to blocked and is audited with
// its reason; it never runs again under the same gate.
func (r DestructionRunner) Run(ctx context.Context) (DestructionResult, error) {
	var res DestructionResult
	if r.DB == nil || r.Store == nil || r.Records == nil || r.Audit == nil || r.Now == nil || r.Zone == nil {
		return res, fmt.Errorf("%s: %w", KindDestructionRun, ErrRunnerDependency)
	}
	for i := 0; i < maxPerPass; i++ {
		var (
			found  bool
			reason string
		)
		err := r.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			o, ok, err := r.Store.NextScheduled(ctx, tx)
			if err != nil || !ok {
				return err
			}
			found = true
			reason, err = r.runOne(ctx, tx, o)
			return err
		})
		if err != nil {
			return res, fmt.Errorf("%s: %w", KindDestructionRun, err)
		}
		if !found {
			return res, nil
		}
		if reason != "" {
			res.Blocked++
		} else {
			res.Done++
		}
	}
	return res, nil
}

// runOne blocks or executes o in tx and returns the block reason ("" when done).
func (r DestructionRunner) runOne(ctx context.Context, tx pgx.Tx, o DestructionOrder) (string, error) {
	m, err := r.Records.Retention(ctx, tx, o.MatterID)
	if err != nil {
		return "", err
	}
	now := r.Now()
	day := now.In(r.Zone)
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	if !m.AlreadyDestroyed {
		if reason := Blocked(o, m, day); reason != "" {
			return reason, r.finish(ctx, tx, o, "blocked", ActionDestructionBlocked+"."+reason)
		}
		if r.Shredder == nil {
			return "", fmt.Errorf("order %s: %w", o.GateID, ErrNoShredder)
		}
		// rule: ARC-I-012
		// Destruction runs only under its approving gate's order, after the checks above.
		if err := r.Shredder.Shred(ctx, tx, o.MatterID, m.KeepSet); err != nil {
			return "", fmt.Errorf("order %s: shred: %w", o.GateID, err)
		}
		if err := r.Records.RecordDestroyed(ctx, tx, o.MatterID, o.GateID, now); err != nil {
			return "", err
		}
	}
	return "", r.finish(ctx, tx, o, "done", ActionDestructionDone)
}

func (r DestructionRunner) finish(ctx context.Context, tx pgx.Tx, o DestructionOrder, status, action string) error {
	moved, err := r.Store.Mark(ctx, tx, o.GateID, status)
	if err != nil || !moved {
		return err
	}
	return r.Audit.Append(ctx, tx, kaudit.Event{ActorKind: runnerActor, ActorID: KindDestructionRun, Action: action,
		Subject: kaudit.Subject{Kind: SubjectDestructionOrder, ID: o.GateID}})
}
