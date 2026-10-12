package timekeeping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// SQL PGEntries runs. A suggestion lives in timekeeping.time_suggestion until its owner
// accepts it; accepting writes the time_entry row (origin suggestion, suggestion_id set).
const (
	entryColumns = `e.id::text, e.matter_id::text, e.user_id::text, e.work_date::text, e.minutes,
  e.billed_minutes, coalesce(e.activity_code_id::text, ''), e.narrative, e.billable, e.origin, e.status,
  coalesce(e.bill_id::text, ''), e.row_version, e.rate_cents`
	entryGetSQL       = `SELECT ` + entryColumns + ` FROM timekeeping.time_entry e WHERE e.id = $1::uuid`
	entryLockSQL      = entryGetSQL + ` FOR UPDATE`
	entryBySuggestSQL = `SELECT ` + entryColumns + ` FROM timekeeping.time_entry e WHERE e.suggestion_id = $1::uuid`
	suggestionColumns = `s.id::text, coalesce(s.matter_id::text, ''), s.user_id::text,
  (s.starts_at AT TIME ZONE 'America/Toronto')::date::text, s.minutes, coalesce(s.activity_code_id::text, ''),
  coalesce(s.narrative, ''), s.status`
	suggestionGetSQL  = `SELECT ` + suggestionColumns + ` FROM timekeeping.time_suggestion s WHERE s.id = $1::uuid`
	suggestionLockSQL = suggestionGetSQL + ` FOR UPDATE`
	suggestInsertSQL  = `
INSERT INTO timekeeping.time_suggestion (user_id, matter_id, starts_at, minutes, activity_code_id, narrative)
VALUES ($1::uuid, $2::uuid, ($3::date)::timestamp AT TIME ZONE 'America/Toronto', $4, nullif($5, '')::uuid, $6)
RETURNING id::text`
	suggestionSetSQL = `UPDATE timekeeping.time_suggestion SET status = $2 WHERE id = $1::uuid`
	entryInsertSQL   = `
INSERT INTO timekeeping.time_entry (matter_id, user_id, work_date, minutes, billed_minutes, activity_code_id,
  narrative, billable, rate_cents, rate_source, amount_cents, origin, suggestion_id, status, accepted_by,
  accepted_at, created_by, updated_by)
VALUES ($1::uuid, $2::uuid, $3::date, $4, $4, nullif($5, '')::uuid, $6, $7, $8, $9, $10, $11,
  nullif($12, '')::uuid, 'accepted', $13::uuid, now(), $13::uuid, $13::uuid)
RETURNING id::text`
	entryStateSQL = `
UPDATE timekeeping.time_entry
   SET status = $2,
       bill_id = CASE WHEN $2 = 'approved' AND status IN ('on_draft_bill', 'billed') THEN NULL ELSE bill_id END,
       accepted_by = CASE WHEN $2 = 'accepted' THEN coalesce(nullif($3, '')::uuid, accepted_by) ELSE accepted_by END,
       accepted_at = CASE WHEN $2 = 'accepted' AND nullif($3, '') IS NOT NULL THEN now() ELSE accepted_at END,
       approved_by = CASE WHEN $2 = 'approved' AND status = 'submitted' THEN nullif($3, '')::uuid ELSE approved_by END,
       approved_at = CASE WHEN $2 = 'approved' AND status = 'submitted' THEN now() ELSE approved_at END,
       updated_by = nullif($3, '')::uuid, updated_at = now(), row_version = row_version + 1
 WHERE id = $1::uuid`
	entryEditSQL = `
UPDATE timekeeping.time_entry
   SET minutes = $2, billed_minutes = $2, narrative = $3, activity_code_id = nullif($4, '')::uuid,
       amount_cents = $5, updated_by = $6::uuid, updated_at = now(), row_version = row_version + 1
 WHERE id = $1::uuid`
	// The rate of a user's work on a day from a rate table (the matter's, else the default
	// table): an activity-specific rate before the user's general one, latest first. Without
	// one, the activity code's own hourly rate.
	entryRateSQL = `
SELECT r.hourly_cents
  FROM timekeeping.rate r
 WHERE r.rate_table_id = coalesce(nullif($1, '')::uuid,
       (SELECT t.id FROM timekeeping.rate_table t WHERE t.is_default ORDER BY t.id LIMIT 1))
   AND r.user_id = $2::uuid AND r.hourly_cents IS NOT NULL AND r.effective_from <= $3::date
   AND (r.activity_code_id IS NULL OR r.activity_code_id = nullif($4, '')::uuid)
 ORDER BY (r.activity_code_id IS NOT NULL) DESC, r.effective_from DESC
 LIMIT 1`
	activityRateSQL = `SELECT rate_cents FROM timekeeping.activity_code WHERE id = $1::uuid AND rate_cents IS NOT NULL`
	entryListSQL    = `
SELECT x.id::text, x.matter_id, x.user_id::text, x.work_date, x.minutes, x.activity_code_id, x.narrative, x.status FROM (
  SELECT e.id, e.matter_id::text AS matter_id, e.user_id, e.work_date::text AS work_date, e.minutes,
         coalesce(e.activity_code_id::text, '') AS activity_code_id, e.narrative, e.status
    FROM timekeeping.time_entry e
  UNION ALL
  SELECT s.id, coalesce(s.matter_id::text, ''), s.user_id, (s.starts_at AT TIME ZONE 'America/Toronto')::date::text,
         s.minutes, coalesce(s.activity_code_id::text, ''), coalesce(s.narrative, ''), 'suggested'
    FROM timekeeping.time_suggestion s WHERE s.status = 'proposed'
) x
WHERE (nullif($1, '')::uuid IS NULL OR x.user_id = nullif($1, '')::uuid)
  AND (nullif($2, '')::uuid IS NULL OR x.id > nullif($2, '')::uuid)
ORDER BY x.id
LIMIT $3`
)

// PGEntries is the Postgres Entries over timekeeping.time_suggestion and time_entry. An
// agent or signal suggestion stays in time_suggestion, state suggested, until its owner
// accepts it; accepting it writes the accepted time entry at the resolved rate. Amounts are
// integer cents. A nil tx runs the call in its own transaction.
type PGEntries struct {
	DB db.DB
	// RateTable names a matter's own rate table ("" for none), read through the matter
	// module. Nil prices every entry from the default rate table.
	RateTable func(ctx context.Context, matterID string) (string, error)
}

var _ Entries = PGEntries{}

// maxEntryMinutes is one day of work (time_entry.minutes CHECK).
const maxEntryMinutes = 1440

func (p PGEntries) run(ctx context.Context, tx pgx.Tx, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if tx != nil {
		return fn(ctx, tx)
	}
	if p.DB == nil {
		return fmt.Errorf("no database: %w", ErrNotFound)
	}
	return p.DB.InTx(ctx, fn)
}

func missing(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02")
}

func checkEntry(e TimeEntry) error {
	if e.MatterID == "" || e.UserID == "" || e.WorkDate.IsZero() || e.Minutes <= 0 || e.Minutes > maxEntryMinutes {
		return ErrInvalid
	}
	return nil
}

// Suggest implements Entries. The suggestion belongs to the user it is made for; it is
// never accepted here.
func (p PGEntries) Suggest(ctx context.Context, tx pgx.Tx, e TimeEntry, by Actor) (EntryID, error) {
	if err := checkEntry(e); err != nil {
		return "", fmt.Errorf("suggest: %w", err)
	}
	// rule: ARC-I-035
	// An agent suggests only for the user it acts for.
	if by.UserID != e.UserID {
		return "", fmt.Errorf("suggest: %w", ErrNotOwner)
	}
	var id string
	err := p.run(ctx, tx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, suggestInsertSQL, e.UserID, e.MatterID, e.WorkDate.Format(time.DateOnly),
			e.Minutes, e.ActivityCode, e.Narrative).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("suggest: %w", err)
	}
	return EntryID(id), nil
}

// Record implements Entries: a human entry by its owner, accepted at once.
func (p PGEntries) Record(ctx context.Context, tx pgx.Tx, e TimeEntry, by Actor) (EntryID, error) {
	if by.Agent {
		return "", fmt.Errorf("record: %w", ErrAgentForbidden)
	}
	if by.UserID != e.UserID {
		return "", fmt.Errorf("record: %w", ErrNotOwner)
	}
	if err := checkEntry(e); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}
	origin := OriginManual
	if e.Origin == OriginTimer {
		origin = OriginTimer
	}
	var id EntryID
	err := p.run(ctx, tx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = p.insertEntry(ctx, tx, e, origin, "", by.UserID)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("record: %w", err)
	}
	return id, nil
}

// insertEntry writes an accepted entry priced at the user's rate on the work date.
func (p PGEntries) insertEntry(ctx context.Context, tx pgx.Tx, e TimeEntry, origin Origin, suggestionID, acceptedBy string) (EntryID, error) {
	day := e.WorkDate.Format(time.DateOnly)
	table := ""
	if p.RateTable != nil {
		var err error
		if table, err = p.RateTable(ctx, e.MatterID); err != nil {
			return "", fmt.Errorf("rate table: %w", err)
		}
	}
	rate, source, err := entryRate(ctx, tx, table, e.UserID, day, e.ActivityCode)
	if err != nil {
		return "", err
	}
	billable := e.Billable || origin == OriginSuggestion
	var id string
	err = tx.QueryRow(ctx, entryInsertSQL, e.MatterID, e.UserID, day, e.Minutes, e.ActivityCode, e.Narrative,
		billable, rate, source, entryAmount(billable, e.Minutes, rate), string(origin), suggestionID, acceptedBy).Scan(&id)
	if err != nil {
		return "", err
	}
	return EntryID(id), nil
}

// entryAmount is minutes at an hourly rate in cents, rounded half up; nil when not billable.
func entryAmount(billable bool, minutes int, rateCents int64) int64 {
	if !billable {
		return 0
	}
	return (int64(minutes)*rateCents + 30) / 60
}

func entryRate(ctx context.Context, tx pgx.Tx, table, userID, day, activity string) (int64, string, error) {
	var rate int64
	err := tx.QueryRow(ctx, entryRateSQL, table, userID, day, activity).Scan(&rate)
	if err == nil {
		if table != "" {
			return rate, "matter_user", nil
		}
		return rate, "user_default", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("rate: %w", err)
	}
	if activity != "" {
		err = tx.QueryRow(ctx, activityRateSQL, activity).Scan(&rate)
		if err == nil {
			return rate, "activity", nil
		}
		if !missing(err) {
			return 0, "", fmt.Errorf("rate: %w", err)
		}
	}
	return 0, "", fmt.Errorf("no rate for user %s on %s: %w", userID, day, ErrInvalid)
}

// loaded is an entry or a pending suggestion, with the stored rate of an entry.
type loaded struct {
	entry      TimeEntry
	suggestion bool
	rateCents  int64
}

func scanEntry(row pgx.Row) (loaded, error) {
	var (
		l              loaded
		e              = &l.entry
		day, org, st   string
		billID, active string
	)
	err := row.Scan(&e.ID, &e.MatterID, &e.UserID, &day, &e.Minutes, &e.BilledMinutes, &active, &e.Narrative,
		&e.Billable, &org, &st, &billID, &e.RowVersion, &l.rateCents)
	if err != nil {
		return l, err
	}
	e.ActivityCode, e.Origin, e.State, e.BillID = active, Origin(org), State(st), billID
	e.WorkDate, err = time.Parse(time.DateOnly, day)
	return l, err
}

func scanSuggestion(row pgx.Row) (loaded, error) {
	l := loaded{suggestion: true}
	e := &l.entry
	var day, status string
	if err := row.Scan(&e.ID, &e.MatterID, &e.UserID, &day, &e.Minutes, &e.ActivityCode, &e.Narrative, &status); err != nil {
		return l, err
	}
	e.BilledMinutes, e.Billable, e.Origin, e.RowVersion = e.Minutes, true, OriginSuggestion, 1
	switch status {
	case "proposed":
		e.State = StateSuggested
	case "accepted":
		e.State = StateAccepted
	default:
		e.State = StateRejected
	}
	var err error
	e.WorkDate, err = time.Parse(time.DateOnly, day)
	return l, err
}

// load reads id as an entry, else as a suggestion. An accepted suggestion reads as the entry
// it became, so the id a gate names keeps resolving.
func load(ctx context.Context, tx pgx.Tx, id EntryID, lock bool) (loaded, error) {
	q, sq := entryGetSQL, suggestionGetSQL
	if lock {
		q, sq = entryLockSQL, suggestionLockSQL
	}
	l, err := scanEntry(tx.QueryRow(ctx, q, string(id)))
	if !missing(err) {
		return l, err
	}
	l, err = scanSuggestion(tx.QueryRow(ctx, sq, string(id)))
	if err != nil {
		if missing(err) {
			return l, ErrNotFound
		}
		return l, err
	}
	if l.entry.State == StateAccepted {
		q := entryBySuggestSQL
		if lock {
			q += ` FOR UPDATE`
		}
		return scanEntry(tx.QueryRow(ctx, q, string(id)))
	}
	return l, nil
}

// Get implements Entries.
func (p PGEntries) Get(ctx context.Context, id EntryID) (TimeEntry, error) {
	var l loaded
	err := p.run(ctx, nil, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		l, err = load(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return TimeEntry{}, fmt.Errorf("get %s: %w", id, err)
	}
	return l.entry, nil
}

// Transition implements Entries. It checks the state machine and who may act; the caller
// opens the gate the edge needs. Entries go on a bill only through the draft builder, which
// sets the bill, so a move to on_draft_bill or billed here is ErrBadTransition.
func (p PGEntries) Transition(ctx context.Context, tx pgx.Tx, id EntryID, to State, by Actor) error {
	err := p.run(ctx, tx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := load(ctx, tx, id, true)
		if err != nil {
			return err
		}
		// rule: ARC-I-035
		// A suggestion is never auto-accepted: only its owner accepts it, never an agent.
		if _, err := CheckTransition(l.entry, to, by); err != nil {
			return err
		}
		if l.suggestion {
			if to == StateRejected {
				_, err := tx.Exec(ctx, suggestionSetSQL, string(l.entry.ID), "rejected")
				return err
			}
			if _, err := tx.Exec(ctx, suggestionSetSQL, string(l.entry.ID), "accepted"); err != nil {
				return err
			}
			_, err := p.insertEntry(ctx, tx, l.entry, OriginSuggestion, string(l.entry.ID), by.UserID)
			return err
		}
		if to == StateOnDraftBill || to == StateBilled {
			return fmt.Errorf("%s -> %s needs a bill: %w", l.entry.State, to, ErrBadTransition)
		}
		_, err = tx.Exec(ctx, entryStateSQL, string(l.entry.ID), string(to), by.UserID)
		return err
	})
	if err != nil {
		return fmt.Errorf("transition %s -> %s: %w", id, to, err)
	}
	return nil
}

// Edit implements Entries: the owner changes minutes, narrative or activity of an entry not
// on a bill; the amount follows at the entry's stored rate.
func (p PGEntries) Edit(ctx context.Context, tx pgx.Tx, e TimeEntry, by Actor) error {
	if by.Agent {
		return fmt.Errorf("edit %s: %w", e.ID, ErrAgentForbidden)
	}
	if e.Minutes <= 0 || e.Minutes > maxEntryMinutes {
		return fmt.Errorf("edit %s: %w", e.ID, ErrInvalid)
	}
	err := p.run(ctx, tx, func(ctx context.Context, tx pgx.Tx) error {
		l, err := scanEntry(tx.QueryRow(ctx, entryLockSQL, string(e.ID)))
		if missing(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		cur := l.entry
		if by.UserID != cur.UserID {
			return ErrNotOwner
		}
		if cur.BillID != "" || cur.State == StateOnDraftBill || cur.State == StateBilled {
			return ErrBilledLocked
		}
		_, err = tx.Exec(ctx, entryEditSQL, string(e.ID), e.Minutes, e.Narrative, e.ActivityCode,
			entryAmount(cur.Billable, e.Minutes, l.rateCents), by.UserID)
		return err
	})
	if err != nil {
		return fmt.Errorf("edit %s: %w", e.ID, err)
	}
	return nil
}

// List implements Entries: entries and pending suggestions by id after f.After.
func (p PGEntries) List(ctx context.Context, f EntryFilter) ([]TimeEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	var out []TimeEntry
	err := p.run(ctx, nil, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, entryListSQL, f.UserID, string(f.After), limit)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (TimeEntry, error) {
			var e TimeEntry
			var day, st string
			if err := r.Scan(&e.ID, &e.MatterID, &e.UserID, &day, &e.Minutes, &e.ActivityCode, &e.Narrative, &st); err != nil {
				return e, err
			}
			e.State, e.BilledMinutes = State(st), e.Minutes
			e.WorkDate, err = time.Parse(time.DateOnly, day)
			return e, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	return out, nil
}
