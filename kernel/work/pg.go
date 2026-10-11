package work

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

var _ Service = (*PG)(nil)

// PG is the Postgres work.Service over work.work_item, work.status and work.transition.
// Writes run in the caller's transaction; Get and List open their own.
type PG struct {
	DB db.DB
	// Kinds are the subject kinds the app registered; an item's subject must be one of them.
	Kinds *SubjectKinds
}

// sqlCreate inserts an item. The key is "W-" plus the random tail of the uuidv7 id, and the
// rank (lexorank) starts as the id, so new items sort in creation order.
const sqlCreate = `WITH n AS (SELECT core.uuidv7() AS id)
INSERT INTO work.work_item (id, key, type, title, workflow_id, status_id, subject_kind, subject_id,
  parent_id, assignee_user_id, assignee_is_agent, reporter_id, priority, due_at, labels, estimate_min, rank)
SELECT n.id, COALESCE(NULLIF($1, ''), 'W-' || upper(right(replace(n.id::text, '-', ''), 12))),
  $2, $3, $4::uuid, $5::uuid, $6, $7::uuid, $8::uuid, $9::uuid, $10, $11::uuid, $12, $13, $14, $15, n.id::text
FROM n RETURNING id::text`

const itemColumns = `id::text, key, type, title, workflow_id::text, status_id::text, subject_kind,
  subject_id::text, parent_id::text, assignee_user_id::text, assignee_is_agent, approver_id::text,
  reporter_id::text, priority, due_at, labels, estimate_min, row_version`

const sqlGet = `SELECT ` + itemColumns + ` FROM work.work_item WHERE id = $1::uuid AND archived_at IS NULL`

// sqlList filters with optional ($n IS NULL) arguments and pages in id order.
const sqlList = `SELECT ` + itemColumns + ` FROM work.work_item
WHERE archived_at IS NULL
  AND ($1::text IS NULL OR (subject_kind = $1 AND subject_id = $2::uuid))
  AND ($3::uuid IS NULL OR assignee_user_id = $3::uuid)
  AND ($4::uuid IS NULL OR status_id = $4::uuid)
  AND ($5::uuid IS NULL OR id > $5::uuid)
ORDER BY id
LIMIT $6`

const sqlLockItem = `SELECT workflow_id::text, status_id::text, assignee_user_id::text, row_version
FROM work.work_item WHERE id = $1::uuid AND archived_at IS NULL FOR UPDATE`

const sqlHasTransition = `SELECT EXISTS (SELECT 1 FROM work.transition
WHERE workflow_id = $1::uuid AND from_status_id = $2::uuid AND to_status_id = $3::uuid)`

const sqlMove = `UPDATE work.work_item SET status_id = $2::uuid, row_version = row_version + 1,
  updated_at = now(), updated_by = $3::uuid
WHERE id = $1::uuid AND row_version = $4`

const sqlSetApprover = `UPDATE work.work_item SET approver_id = $2::uuid, row_version = row_version + 1,
  updated_at = now()
WHERE id = $1::uuid`

// maxListLimit caps one List page when the filter sets no limit.
const maxListLimit = 1000

// Create implements Service. The item is validated with PrepareItem.
func (p *PG) Create(ctx context.Context, tx pgx.Tx, it Item) (ItemID, error) {
	it, err := p.Kinds.PrepareItem(it)
	if err != nil {
		return "", fmt.Errorf("create work item: %w", err)
	}
	for name, v := range map[string]string{
		"workflow": it.WorkflowID, "status": it.StatusID, "reporter": it.ReporterID,
		"assignee": it.AssigneeUserID, "parent": string(it.ParentID),
	} {
		if v != "" && !uuidPattern.MatchString(v) {
			return "", fmt.Errorf("create work item: %s id %q not a uuid: %w", name, v, ErrInvalidItem)
		}
	}
	if it.AssigneeIsAgent && it.AssigneeUserID == "" {
		// Agent work is on behalf of a user (work_item check).
		return "", fmt.Errorf("create work item: agent assignee without a user: %w", ErrInvalidItem)
	}
	if it.EstimateMin < 0 {
		return "", fmt.Errorf("create work item: negative estimate: %w", ErrInvalidItem)
	}
	labels := it.Labels
	if labels == nil {
		labels = []string{}
	}
	kind, sid := SubjectColumns(it.Subject)
	var id string
	err = tx.QueryRow(ctx, sqlCreate,
		it.Key, it.Type, it.Title, it.WorkflowID, it.StatusID, kind, sid,
		nullable(string(it.ParentID)), nullable(it.AssigneeUserID), it.AssigneeIsAgent, it.ReporterID,
		it.Priority, it.DueAt, labels, nullableInt(it.EstimateMin),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create work item: %w", err)
	}
	return ItemID(id), nil
}

// Get implements Service. A malformed id is ErrNotFound.
func (p *PG) Get(ctx context.Context, id ItemID) (Item, error) {
	if !uuidPattern.MatchString(string(id)) {
		return Item{}, fmt.Errorf("get work item %s: %w", id, ErrNotFound)
	}
	var it Item
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRow(ctx, sqlGet, string(id)))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("get work item %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Item{}, fmt.Errorf("get work item %s: %w", id, err)
	}
	return it, nil
}

// List implements Service: items in id order, after f.After, at most f.Limit (default and
// cap maxListLimit). A malformed filter id matches nothing.
func (p *PG) List(ctx context.Context, f Filter) ([]Item, error) {
	for _, v := range []string{f.Subject.ID, f.AssigneeUserID, f.StatusID, string(f.After)} {
		if v != "" && !uuidPattern.MatchString(v) {
			return nil, nil
		}
	}
	limit := f.Limit
	if limit <= 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	kind, sid := SubjectColumns(f.Subject)
	if f.Subject.Kind == "" {
		kind, sid = nil, nil
	}
	var out []Item
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlList, kind, sid, nullable(f.AssigneeUserID), nullable(f.StatusID),
			nullable(string(f.After)), limit)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	return out, nil
}

// lockItem locks the item row for the rest of tx.
func lockItem(ctx context.Context, tx pgx.Tx, id ItemID) (workflow, status, assignee string, version int, err error) {
	if !uuidPattern.MatchString(string(id)) {
		return "", "", "", 0, ErrNotFound
	}
	var a *string
	err = tx.QueryRow(ctx, sqlLockItem, string(id)).Scan(&workflow, &status, &a, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", 0, ErrNotFound
	}
	if a != nil {
		assignee = *a
	}
	return workflow, status, assignee, version, err
}

// Move implements Service. by is recorded as updated_by when it is a user id.
func (p *PG) Move(ctx context.Context, tx pgx.Tx, id ItemID, version int, toStatusID string, by string) error {
	workflow, from, _, current, err := lockItem(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("move work item %s: %w", id, err)
	}
	if current != version {
		return fmt.Errorf("move work item %s: version %d, current %d: %w", id, version, current, ErrStale)
	}
	if !uuidPattern.MatchString(toStatusID) {
		return fmt.Errorf("move work item %s to %q: %w", id, toStatusID, ErrTransitionNotAllowed)
	}
	var ok bool
	if err := tx.QueryRow(ctx, sqlHasTransition, workflow, from, toStatusID).Scan(&ok); err != nil {
		return fmt.Errorf("move work item %s: transition: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("move work item %s from %s to %s: %w", id, from, toStatusID, ErrTransitionNotAllowed)
	}
	var updatedBy *string
	if uuidPattern.MatchString(by) {
		updatedBy = &by
	}
	tag, err := tx.Exec(ctx, sqlMove, string(id), toStatusID, updatedBy, version)
	if err != nil {
		return fmt.Errorf("move work item %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("move work item %s: %w", id, ErrStale)
	}
	return nil
}

// SetApprover implements Service.
func (p *PG) SetApprover(ctx context.Context, tx pgx.Tx, id ItemID, approverID string, approverIsAgent bool) error {
	// rule: the agent never approves.
	if approverIsAgent || strings.TrimSpace(approverID) == "" {
		return fmt.Errorf("set approver on %s: %w", id, ErrSelfApproval)
	}
	if !uuidPattern.MatchString(approverID) {
		return fmt.Errorf("set approver on %s: approver %q not a uuid: %w", id, approverID, ErrInvalidItem)
	}
	_, _, assignee, _, err := lockItem(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("set approver on %s: %w", id, err)
	}
	// rule: the person who prepares an item never approves it.
	if strings.EqualFold(approverID, assignee) {
		return fmt.Errorf("set approver on %s: %w", id, ErrSelfApproval)
	}
	if _, err := tx.Exec(ctx, sqlSetApprover, string(id), approverID); err != nil {
		return fmt.Errorf("set approver on %s: %w", id, err)
	}
	return nil
}

func scanItem(row pgx.Row) (Item, error) {
	var (
		it                                    Item
		id                                    string
		kind, sid, parent, assignee, approver *string
		estimate                              *int32
		priority                              int16
		due                                   *time.Time
	)
	err := row.Scan(&id, &it.Key, &it.Type, &it.Title, &it.WorkflowID, &it.StatusID, &kind, &sid,
		&parent, &assignee, &it.AssigneeIsAgent, &approver, &it.ReporterID, &priority, &due,
		&it.Labels, &estimate, &it.Version)
	if err != nil {
		return Item{}, err
	}
	it.ID = ItemID(id)
	it.Subject = SubjectFromColumns(kind, sid)
	it.ParentID = ItemID(deref(parent))
	it.AssigneeUserID = deref(assignee)
	it.ApproverID = deref(approver)
	it.Priority = int(priority)
	it.DueAt = due
	if estimate != nil {
		it.EstimateMin = int(*estimate)
	}
	return it, nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
