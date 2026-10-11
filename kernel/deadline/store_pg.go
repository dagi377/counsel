package deadline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
)

var _ Engine = (*PGBook)(nil)

// TxRunner runs fn in one database transaction. kernel/db.DB satisfies it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// PGBook is Book over cal.deadline and its append-only cal.deadline_confirmation log
// (docs/QUESTIONS.md Q-PORT-20). It reads and writes only the generic columns: the subject
// pair, the pack code and version, the rule key and the trigger date. Loaded packs are held
// in memory. Save, Confirm, Confirmations and Repack run on the caller's transaction; Get
// runs its own.
type PGBook struct {
	db    TxRunner
	mu    sync.Mutex
	packs map[string]Pack
	// Events receives the rule-pack event Repack emits in its tx. Boot sets it; while nil,
	// Repack refuses to write (a pack change nobody hears about would leave dependants stale).
	Events outbox.Emitter
}

// EventPackPublished is the outbox event type Repack emits when a new pack version is
// applied. Subject is SubjectRulePack with the pack id.
const EventPackPublished = "rulepack.published"

// SubjectRulePack is the outbox subject kind of a rule pack.
const SubjectRulePack = "cal.rule_pack"

// PackPublished is the body of EventPackPublished: ids and counts only.
type PackPublished struct {
	PackID     string `json:"pack_id"`
	Version    string `json:"version"`
	Recomputed int    `json:"recomputed"`
}

// NewPGBook returns a store that runs Get through db.
func NewPGBook(db TxRunner) *PGBook {
	return &PGBook{db: db, packs: map[string]Pack{}}
}

// LoadPack checks p's shape and registers it, replacing an older version of the same pack.
func (b *PGBook) LoadPack(_ context.Context, p Pack) error {
	if p.ID == "" || p.Version == "" {
		return fmt.Errorf("load pack: missing id or version: %w", ErrInvalidPack)
	}
	for _, r := range p.Rules {
		switch r.Mode {
		case Calendar, Clear, Business:
		default:
			return fmt.Errorf("load pack %s rule %s mode %q: %w", p.ID, r.ID, r.Mode, ErrInvalidPack)
		}
		if r.ID == "" || r.Days < 0 || r.MonthsAfter < 0 {
			return fmt.Errorf("load pack %s rule %q: %w", p.ID, r.ID, ErrInvalidPack)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.packs[p.ID] = p
	return nil
}

// Compute returns the proposal of rule of the loaded pack for an event on trigger.
func (b *PGBook) Compute(_ context.Context, pack, rule string, trigger Date, subject Subject) ([]Proposed, error) {
	b.mu.Lock()
	p, ok := b.packs[pack]
	b.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("compute %s: %w", pack, ErrUnknownPack)
	}
	due, err := Due(p, rule, trigger)
	if err != nil {
		return nil, fmt.Errorf("compute: %w", err)
	}
	return []Proposed{{PackID: p.ID, PackVersion: p.Version, RuleID: rule, Trigger: trigger, Due: due, Subject: subject}}, nil
}

const saveSQL = `INSERT INTO cal.deadline
  (subject_kind, subject_id, pack_code, pack_version, rule_key, trigger_on, due_on, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'proposed')
RETURNING id::text`

// Save stores p in state proposed and returns its id.
func (b *PGBook) Save(ctx context.Context, tx pgx.Tx, p Proposed) (DeadlineID, error) {
	if p.PackID == "" || p.PackVersion == "" || p.RuleID == "" {
		return "", fmt.Errorf("save deadline: missing pack or rule: %w", ErrInvalidPack)
	}
	if p.Subject.Kind == "" || !isUUID(p.Subject.ID) {
		return "", fmt.Errorf("save deadline: subject %q needs a kind and a uuid id: %w", p.Subject.Kind, ErrInvalidPack)
	}
	var id string
	err := tx.QueryRow(ctx, saveSQL, p.Subject.Kind, p.Subject.ID, p.PackID, p.PackVersion, p.RuleID,
		dateArg(p.Trigger), dateArg(p.Due)).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("save deadline: %w", err)
	}
	return DeadlineID(id), nil
}

const lockSQL = `SELECT status, due_on FROM cal.deadline WHERE id = $1 FOR UPDATE`

const confirmSQL = `UPDATE cal.deadline
   SET status = 'confirmed', updated_by = $2, updated_at = now(), row_version = row_version + 1
 WHERE id = $1 AND status = 'proposed'`

const logSQL = `INSERT INTO cal.deadline_confirmation (deadline_id, kind, user_id, independent_date, result)
VALUES ($1, 'confirm', $2, $3, 'agree')`

// Confirm records by's confirmation of deadline id. Only a person (a user principal with a
// user id) confirms; an agent, a guest or an anonymous principal is refused. A deadline that
// is no longer proposed is refused with ErrAlreadyConfirmed.
func (b *PGBook) Confirm(ctx context.Context, tx pgx.Tx, id DeadlineID, by Principal) error {
	// Fail closed: anything that is not a named user is refused.
	if by.Kind != registry.PrincipalUser || by.UserID == "" {
		return fmt.Errorf("confirm %s: %w", id, ErrAgentCannotConfirm)
	}
	if !isUUID(string(id)) {
		return fmt.Errorf("confirm %s: %w", id, ErrNotFound)
	}
	var status string
	var due time.Time
	if err := tx.QueryRow(ctx, lockSQL, string(id)).Scan(&status, &due); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("confirm %s: %w", id, ErrNotFound)
		}
		return fmt.Errorf("confirm %s: lock: %w", id, err)
	}
	if State(status) != StateProposed {
		return fmt.Errorf("confirm %s: %w", id, ErrAlreadyConfirmed)
	}
	tag, err := tx.Exec(ctx, confirmSQL, string(id), by.UserID)
	if err != nil {
		return fmt.Errorf("confirm %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("confirm %s: %w", id, ErrAlreadyConfirmed)
	}
	if _, err := tx.Exec(ctx, logSQL, string(id), by.UserID, due); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return fmt.Errorf("confirm %s: %w", id, ErrAlreadyConfirmed)
		}
		return fmt.Errorf("confirm %s: log: %w", id, err)
	}
	return nil
}

const getSQL = `SELECT d.subject_kind, d.subject_id::text, d.pack_code, d.pack_version, d.rule_key,
       d.trigger_on, d.due_on, d.status,
       (SELECT c.user_id::text FROM cal.deadline_confirmation c
         WHERE c.deadline_id = d.id AND c.kind = 'confirm' ORDER BY c.at, c.id LIMIT 1)
  FROM cal.deadline d
 WHERE d.id = $1`

// Get returns one stored deadline. It runs in its own transaction. State is the stored
// status as is; rows the app moved past confirmed keep the app's status.
func (b *PGBook) Get(ctx context.Context, id DeadlineID) (Deadline, error) {
	if !isUUID(string(id)) {
		return Deadline{}, fmt.Errorf("get %s: %w", id, ErrNotFound)
	}
	var d Deadline
	err := b.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var kind, subject, pack, version, rule, by *string
		var trigger *time.Time
		var due time.Time
		var status string
		err := tx.QueryRow(ctx, getSQL, string(id)).Scan(&kind, &subject, &pack, &version, &rule,
			&trigger, &due, &status, &by)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		d = Deadline{ID: id, State: State(status), ConfirmedBy: deref(by), Proposed: Proposed{
			PackID: deref(pack), PackVersion: deref(version), RuleID: deref(rule),
			Due:     dateOf(due),
			Subject: Subject{Kind: deref(kind), ID: deref(subject)},
		}}
		if trigger != nil {
			d.Proposed.Trigger = dateOf(*trigger)
		}
		return nil
	})
	if err != nil {
		return Deadline{}, fmt.Errorf("get %s: %w", id, err)
	}
	return d, nil
}

const confirmationsSQL = `SELECT user_id::text, independent_date
  FROM cal.deadline_confirmation
 WHERE deadline_id = $1 AND kind = 'confirm'
 ORDER BY at, id`

// Confirmations returns the confirmation log entries for id, oldest first.
func (b *PGBook) Confirmations(ctx context.Context, tx pgx.Tx, id DeadlineID) ([]Confirmation, error) {
	if !isUUID(string(id)) {
		return nil, nil
	}
	rows, err := tx.Query(ctx, confirmationsSQL, string(id))
	if err != nil {
		return nil, fmt.Errorf("confirmations %s: %w", id, err)
	}
	defer rows.Close()
	var out []Confirmation
	for rows.Next() {
		var user string
		var due *time.Time
		if err := rows.Scan(&user, &due); err != nil {
			return nil, fmt.Errorf("confirmations %s: %w", id, err)
		}
		c := Confirmation{DeadlineID: id, UserID: user}
		if due != nil {
			c.Due = dateOf(*due)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("confirmations %s: %w", id, err)
	}
	return out, nil
}

const repackSelectSQL = `SELECT id::text, rule_key, trigger_on
  FROM cal.deadline
 WHERE pack_code = $1 AND pack_version <> $2 AND status = 'proposed'
 ORDER BY id
 FOR UPDATE`

const repackUpdateSQL = `UPDATE cal.deadline
   SET pack_version = $2, due_on = $3, updated_at = now(), row_version = row_version + 1
 WHERE id = $1 AND status = 'proposed'`

// Repack applies a new version of pack p to the stored deadlines of that pack. Unconfirmed
// deadlines are re-computed from p and take its version; confirmed deadlines are never
// touched (the update itself is guarded by status). It returns the ids it re-computed,
// sorted. Every due date is computed before any row changes, so if one deadline cannot be
// re-computed nothing is written and the error is returned. It emits EventPackPublished in
// tx; without Events it writes nothing.
func (b *PGBook) Repack(ctx context.Context, tx pgx.Tx, p Pack) ([]DeadlineID, error) {
	if p.ID == "" || p.Version == "" {
		return nil, fmt.Errorf("repack: %w", ErrInvalidPack)
	}
	if b.Events == nil {
		return nil, fmt.Errorf("repack %s: no event emitter: %w", p.ID, ErrInvalidPack)
	}
	rows, err := tx.Query(ctx, repackSelectSQL, p.ID, p.Version)
	if err != nil {
		return nil, fmt.Errorf("repack %s: %w", p.ID, err)
	}
	type change struct {
		id  string
		due Date
	}
	var changes []change
	for rows.Next() {
		var id, rule string
		var trigger time.Time
		if err := rows.Scan(&id, &rule, &trigger); err != nil {
			rows.Close()
			return nil, fmt.Errorf("repack %s: %w", p.ID, err)
		}
		due, err := Due(p, rule, dateOf(trigger))
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("repack %s: %w", id, err)
		}
		changes = append(changes, change{id, due})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repack %s: %w", p.ID, err)
	}
	ids := make([]DeadlineID, 0, len(changes))
	for _, c := range changes {
		tag, err := tx.Exec(ctx, repackUpdateSQL, c.id, p.Version, dateArg(c.due))
		if err != nil {
			return nil, fmt.Errorf("repack %s: %w", c.id, err)
		}
		if tag.RowsAffected() == 1 {
			ids = append(ids, DeadlineID(c.id))
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	// The event commits with the re-computed rows, so the recompute feature hears of every
	// published pack version and of nothing that rolled back.
	ev := PackPublished{PackID: p.ID, Version: p.Version, Recomputed: len(ids)}
	if err := b.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectRulePack, ID: p.ID}, EventPackPublished, ev); err != nil {
		return nil, fmt.Errorf("repack %s: emit: %w", p.ID, err)
	}
	return ids, nil
}

// dateArg is d as a date parameter (midnight UTC; Postgres keeps only the date).
func dateArg(d Date) time.Time { return d.Start(time.UTC) }

// dateOf is the civil date of a scanned date column.
func dateOf(t time.Time) Date {
	y, m, day := t.Date()
	return Date{Year: y, Month: m, Day: day}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// isUUID reports whether s has the canonical 8-4-4-4-12 hex form, so a malformed id is
// "not found" rather than a database error.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// EventTriggerDateChanged is the outbox event type ChangeTrigger emits when a stored
// deadline's trigger date changes. Subject is SubjectDeadline with the deadline id.
const EventTriggerDateChanged = "deadline.trigger_date_changed"

// SubjectDeadline is the outbox subject kind of a stored deadline.
const SubjectDeadline = "cal.deadline"

// TriggerDateChanged is the body of EventTriggerDateChanged: ids and dates only. OldTrigger
// is empty when the deadline had no trigger date.
type TriggerDateChanged struct {
	DeadlineID  string `json:"deadline_id"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	PackID      string `json:"pack_id"`
	RuleID      string `json:"rule_id"`
	OldTrigger  string `json:"old_trigger,omitempty"`
	NewTrigger  string `json:"new_trigger"`
}

const triggerLockSQL = `SELECT trigger_on, subject_kind, subject_id::text, pack_code, rule_key
  FROM cal.deadline WHERE id = $1 FOR UPDATE`

const triggerUpdateSQL = `UPDATE cal.deadline
   SET trigger_on = $2, updated_at = now(), row_version = row_version + 1
 WHERE id = $1`

// ChangeTrigger sets the trigger date of stored deadline id to trigger and emits
// EventTriggerDateChanged in tx. The due date is not touched: the dependants are
// recomputed and proposed for confirmation by whoever handles the event, and the old due
// date stays in force until then. An unchanged date writes and emits nothing. Without
// Events it writes nothing.
func (b *PGBook) ChangeTrigger(ctx context.Context, tx pgx.Tx, id DeadlineID, trigger Date) error {
	if b.Events == nil {
		return fmt.Errorf("change trigger %s: no event emitter: %w", id, ErrInvalidPack)
	}
	if trigger == (Date{}) {
		return fmt.Errorf("change trigger %s: no date: %w", id, ErrInvalidPack)
	}
	if !isUUID(string(id)) {
		return fmt.Errorf("change trigger %s: %w", id, ErrNotFound)
	}
	var old *time.Time
	var kind, subject, pack, rule string
	err := tx.QueryRow(ctx, triggerLockSQL, string(id)).Scan(&old, &kind, &subject, &pack, &rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("change trigger %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("change trigger %s: %w", id, err)
	}
	ev := TriggerDateChanged{DeadlineID: string(id), SubjectKind: kind, SubjectID: subject,
		PackID: pack, RuleID: rule, NewTrigger: trigger.String()}
	if old != nil {
		if dateOf(*old) == trigger {
			return nil
		}
		ev.OldTrigger = dateOf(*old).String()
	}
	if _, err := tx.Exec(ctx, triggerUpdateSQL, string(id), dateArg(trigger)); err != nil {
		return fmt.Errorf("change trigger %s: %w", id, err)
	}
	// The event commits with the new trigger date, so F-DL-03 hears of every change and of
	// nothing that rolled back.
	if err := b.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectDeadline, ID: string(id)}, EventTriggerDateChanged, ev); err != nil {
		return fmt.Errorf("change trigger %s: emit: %w", id, err)
	}
	return nil
}
