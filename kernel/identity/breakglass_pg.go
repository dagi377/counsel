package identity

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// GrantStore keeps break-glass grants. Insert and ExpireDue run in the caller's transaction
// and call their callback (the audit write) inside it, so a grant and its audit event commit
// or roll back together.
type GrantStore interface {
	// Insert assigns g an id, calls audit with the stored grant and keeps it if audit succeeds.
	Insert(ctx context.Context, tx pgx.Tx, g BreakGlassGrant, audit func(BreakGlassGrant) error) (BreakGlassGrant, error)
	// Active returns the newest grant in force at now for user on s.
	Active(ctx context.Context, user UserID, s Subject, now time.Time) (BreakGlassGrant, bool, error)
	// ExpireDue calls expire for every grant ended by now and not yet expired, and marks each
	// one expired. It returns how many it expired.
	ExpireDue(ctx context.Context, tx pgx.Tx, now time.Time, expire func(BreakGlassGrant) error) (int, error)
	// List returns every grant in the order opened.
	List(ctx context.Context) ([]BreakGlassGrant, error)
}

// memGrants is the in-memory GrantStore NewBreakGlass uses by default.
type memGrants struct {
	mu     sync.Mutex
	seq    int
	grants []*BreakGlassGrant
}

func newMemGrants() *memGrants { return &memGrants{} }

func (m *memGrants) Insert(_ context.Context, _ pgx.Tx, g BreakGlassGrant, audit func(BreakGlassGrant) error) (BreakGlassGrant, error) {
	// The 8h maximum holds in every store, not only in Open.
	if g.EndsAt.Sub(g.StartsAt) <= 0 || g.EndsAt.Sub(g.StartsAt) > BreakGlassMax {
		return BreakGlassGrant{}, fmt.Errorf("insert grant: %w", ErrInvalidBreakGlass)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g.ID = "bg-" + strconv.Itoa(m.seq+1)
	if err := audit(g); err != nil {
		return BreakGlassGrant{}, err
	}
	m.seq++
	m.grants = append(m.grants, &g)
	return g, nil
}

func (m *memGrants) Active(_ context.Context, user UserID, s Subject, now time.Time) (BreakGlassGrant, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.grants {
		if g.UserID == user && g.Subject == s && g.ActiveAt(now) {
			return *g, true, nil
		}
	}
	return BreakGlassGrant{}, false, nil
}

func (m *memGrants) ExpireDue(_ context.Context, _ pgx.Tx, now time.Time, expire func(BreakGlassGrant) error) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, g := range m.grants {
		if g.Expired || now.Before(g.EndsAt) {
			continue
		}
		if err := expire(*g); err != nil {
			return n, err
		}
		g.Expired = true
		n++
	}
	return n, nil
}

func (m *memGrants) List(context.Context) ([]BreakGlassGrant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]BreakGlassGrant, len(m.grants))
	for i, g := range m.grants {
		out[i] = *g
	}
	return out, nil
}

var _ GrantStore = PGGrants{}

// PGGrants is a GrantStore over audit.break_glass. The frozen table holds grants on one
// subject kind, in an app-named uuid column, so the app sets SubjectKind and SubjectColumn at
// boot; PGGrants stores only SubjectKind grants and refuses the rest. The table CHECK caps a
// grant at 8 hours, like BreakGlassMax.
//
// A grant counts as expired once its expired_at is set (migration 0200). ExpireDue sets it in
// the caller's transaction, after the expiry audit event, so a grant is audited once.
type PGGrants struct {
	DB            db.DB
	SubjectKind   string
	SubjectColumn string // the audit.break_glass column that holds the subject id
}

// ErrNoSubjectColumn means PGGrants was built without SubjectColumn.
var ErrNoSubjectColumn = errors.New("identity: break-glass store has no subject column")

// stmt builds one statement on the subject column. %[1]s is the quoted column, %[2]s the
// selected grant columns.
func (s PGGrants) stmt(f string) (string, error) {
	if s.SubjectColumn == "" {
		return "", ErrNoSubjectColumn
	}
	col := pgx.Identifier{s.SubjectColumn}.Sanitize()
	cols := `bg.id::text, bg.user_id::text, bg.` + col + `::text, bg.reason, bg.explanation, bg.starts_at, bg.ends_at, bg.expired_at IS NOT NULL`
	return fmt.Sprintf(f, col, cols), nil
}

const (
	sqlGrantInsert = `INSERT INTO audit.break_glass (user_id, %[1]s, reason, explanation, starts_at, ends_at)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6) RETURNING id::text`
	sqlGrantActive = `SELECT %[2]s FROM audit.break_glass bg
WHERE bg.user_id = $1::uuid AND bg.%[1]s = $2::uuid AND bg.starts_at <= $3 AND bg.ends_at > $3
ORDER BY bg.starts_at DESC, bg.id DESC LIMIT 1`
	sqlGrantDue = `SELECT %[2]s FROM audit.break_glass bg
WHERE bg.ends_at <= $1 AND bg.expired_at IS NULL
ORDER BY bg.ends_at, bg.id FOR UPDATE OF bg`
	sqlGrantList = `SELECT %[2]s FROM audit.break_glass bg ORDER BY bg.starts_at, bg.id`
	// sqlGrantExpire marks one grant expired; a grant already marked is left as it is.
	sqlGrantExpire = `UPDATE audit.break_glass SET expired_at = $2 WHERE id = $1::uuid AND expired_at IS NULL`
)

// ErrNoTx means a PGGrants write was called without the caller's transaction.
var ErrNoTx = errors.New("identity: break-glass write needs a transaction")

// Insert implements GrantStore.
func (s PGGrants) Insert(ctx context.Context, tx pgx.Tx, g BreakGlassGrant, audit func(BreakGlassGrant) error) (BreakGlassGrant, error) {
	switch {
	case tx == nil:
		return BreakGlassGrant{}, fmt.Errorf("insert grant: %w", ErrNoTx)
	case g.Subject.Kind != s.SubjectKind:
		return BreakGlassGrant{}, fmt.Errorf("insert grant: subject kind %q not stored: %w", g.Subject.Kind, ErrInvalidBreakGlass)
	// The 8h maximum is enforced here and by the audit.break_glass CHECK.
	case g.EndsAt.Sub(g.StartsAt) <= 0 || g.EndsAt.Sub(g.StartsAt) > BreakGlassMax:
		return BreakGlassGrant{}, fmt.Errorf("insert grant: %w", ErrInvalidBreakGlass)
	}
	sql, err := s.stmt(sqlGrantInsert)
	if err != nil {
		return BreakGlassGrant{}, fmt.Errorf("insert grant: %w", err)
	}
	if err := tx.QueryRow(ctx, sql, string(g.UserID), g.Subject.ID, g.Reason, g.Explanation, g.StartsAt, g.EndsAt).Scan(&g.ID); err != nil {
		return BreakGlassGrant{}, fmt.Errorf("insert grant: %w", err)
	}
	if err := audit(g); err != nil {
		return BreakGlassGrant{}, err
	}
	return g, nil
}

// Active implements GrantStore.
func (s PGGrants) Active(ctx context.Context, user UserID, sub Subject, now time.Time) (BreakGlassGrant, bool, error) {
	if sub.Kind != s.SubjectKind {
		return BreakGlassGrant{}, false, nil
	}
	var gs []BreakGlassGrant
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gs, err = s.scan(ctx, tx, sqlGrantActive, string(user), sub.ID, now)
		return err
	})
	if err != nil {
		return BreakGlassGrant{}, false, fmt.Errorf("active grant: %w", err)
	}
	if len(gs) == 0 {
		return BreakGlassGrant{}, false, nil
	}
	return gs[0], true, nil
}

// ExpireDue implements GrantStore. It locks the due rows, so two runners do not expire the
// same grant, and sets expired_at on each one after expire (its audit event) succeeds.
func (s PGGrants) ExpireDue(ctx context.Context, tx pgx.Tx, now time.Time, expire func(BreakGlassGrant) error) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("expire grants: %w", ErrNoTx)
	}
	gs, err := s.scan(ctx, tx, sqlGrantDue, now)
	if err != nil {
		return 0, fmt.Errorf("expire grants: %w", err)
	}
	n := 0
	for _, g := range gs {
		if err := expire(g); err != nil {
			return n, err
		}
		tag, err := tx.Exec(ctx, sqlGrantExpire, g.ID, now)
		if err != nil {
			return n, fmt.Errorf("expire grant %s: %w", g.ID, err)
		}
		// The row is locked FOR UPDATE, so it is still unmarked; anything else fails closed
		// and rolls back the audit event with it.
		if tag.RowsAffected() != 1 {
			return n, fmt.Errorf("expire grant %s: %w", g.ID, ErrGrantNotMarked)
		}
		n++
	}
	return n, nil
}

// ErrGrantNotMarked means ExpireDue could not set expired_at on a grant it had locked.
var ErrGrantNotMarked = errors.New("identity: break-glass grant not marked expired")

// List implements GrantStore.
func (s PGGrants) List(ctx context.Context) ([]BreakGlassGrant, error) {
	var gs []BreakGlassGrant
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gs, err = s.scan(ctx, tx, sqlGrantList)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	return gs, nil
}

func (s PGGrants) scan(ctx context.Context, tx pgx.Tx, f string, args ...any) ([]BreakGlassGrant, error) {
	sql, err := s.stmt(f)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var out []BreakGlassGrant
	for rows.Next() {
		var (
			g    BreakGlassGrant
			user string
		)
		if err := rows.Scan(&g.ID, &user, &g.Subject.ID, &g.Reason, &g.Explanation, &g.StartsAt, &g.EndsAt, &g.Expired); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		g.UserID, g.Subject.Kind = UserID(user), s.SubjectKind
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}
