package identity_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// grantRow is one scripted audit.break_glass row.
type grantRow struct {
	id, user, subject   string
	reason, explanation string
	starts, ends        time.Time
	expiredAt           *time.Time
}

// grantTable answers PGGrants' statements from scripted rows. The table CHECK (ends after
// starts, at most 8 hours) is applied on insert, and a grant counts as expired once its
// expired_at is set, as the real query reads audit.break_glass.expired_at.
type grantTable struct {
	pgx.Tx
	mu   sync.Mutex
	rows []grantRow
	log  *auditfake.Fake
	fail error
	// noMark makes the expiry UPDATE match no row, as if another writer had marked it.
	noMark bool
}

func (f *grantTable) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return grantErrRow{f.fail}
	}
	if !strings.HasPrefix(sql, "INSERT INTO audit.break_glass") {
		return grantErrRow{errors.New("grantTable: unexpected statement")}
	}
	starts, ends := args[4].(time.Time), args[5].(time.Time)
	if !ends.After(starts) || ends.Sub(starts) > 8*time.Hour {
		return grantErrRow{errors.New("ERROR: new row violates check constraint (SQLSTATE 23514)")}
	}
	r := grantRow{
		id:   fmt.Sprintf("00000000-0000-0000-0000-%012d", len(f.rows)+1),
		user: args[0].(string), subject: args[1].(string),
		reason: args[2].(string), explanation: args[3].(string),
		starts: starts, ends: ends,
	}
	f.rows = append(f.rows, r)
	return grantIDRow{r.id}
}

func (f *grantTable) expired(id string) bool {
	for _, r := range f.rows {
		if r.id == id {
			return r.expiredAt != nil
		}
	}
	return false
}

// Exec answers the expiry UPDATE: it sets expired_at on an unmarked row.
func (f *grantTable) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return pgconn.CommandTag{}, f.fail
	}
	if !strings.HasPrefix(sql, "UPDATE audit.break_glass SET expired_at") || !strings.Contains(sql, "expired_at IS NULL") {
		return pgconn.CommandTag{}, errors.New("grantTable: unexpected statement")
	}
	id, at := args[0].(string), args[1].(time.Time)
	for i := range f.rows {
		if f.rows[i].id == id && f.rows[i].expiredAt == nil && !f.noMark {
			f.rows[i].expiredAt = &at
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
	}
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

func (f *grantTable) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := &grantRows{}
	switch {
	case strings.Contains(sql, "LIMIT 1"): // active
		user, subject, now := args[0].(string), args[1].(string), args[2].(time.Time)
		for i := len(f.rows) - 1; i >= 0; i-- {
			r := f.rows[i]
			if r.user == user && r.subject == subject && !r.starts.After(now) && r.ends.After(now) {
				out.rows = append(out.rows, r)
				break
			}
		}
	case strings.Contains(sql, "FOR UPDATE"): // due
		now := args[0].(time.Time)
		for _, r := range f.rows {
			if !r.ends.After(now) && !f.expired(r.id) {
				out.rows = append(out.rows, r)
			}
		}
	default: // list
		out.rows = append(out.rows, f.rows...)
	}
	for _, r := range out.rows {
		out.expired = append(out.expired, f.expired(r.id))
	}
	return out, nil
}

type grantErrRow struct{ err error }

func (r grantErrRow) Scan(...any) error { return r.err }

type grantIDRow struct{ id string }

func (r grantIDRow) Scan(dest ...any) error { *dest[0].(*string) = r.id; return nil }

type grantRows struct {
	pgx.Rows
	rows    []grantRow
	expired []bool
	i       int
}

func (r *grantRows) Next() bool { r.i++; return r.i <= len(r.rows) }
func (r *grantRows) Scan(dest ...any) error {
	if len(dest) != 8 {
		return errors.New("grantRows: want 8 columns")
	}
	g := r.rows[r.i-1]
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = g.id, g.user, g.subject
	*dest[3].(*string), *dest[4].(*string) = g.reason, g.explanation
	*dest[5].(*time.Time), *dest[6].(*time.Time) = g.starts, g.ends
	*dest[7].(*bool) = r.expired[r.i-1]
	return nil
}
func (r *grantRows) Err() error { return nil }
func (r *grantRows) Close()     {}

type grantDB struct{ tx *grantTable }

func (d grantDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (grantDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (grantDB) Ping(context.Context) error                     { return nil }
func (grantDB) Close()                                         {}

// breakGlassStores builds a BreakGlass over each store. tx is what callers pass to Open and
// Expire: nil for memory (no database), the scripted table for Postgres.
type bgStore struct {
	name string
	new  func(clk *clockfake.Fake, log *auditfake.Fake) (*identity.BreakGlass, pgx.Tx, *grantTable)
}

func bgStores() []bgStore {
	return []bgStore{
		{"memory", func(clk *clockfake.Fake, log *auditfake.Fake) (*identity.BreakGlass, pgx.Tx, *grantTable) {
			return identity.NewBreakGlass(clk, log), nil, nil
		}},
		{"postgres", func(clk *clockfake.Fake, log *auditfake.Fake) (*identity.BreakGlass, pgx.Tx, *grantTable) {
			tbl := &grantTable{log: log}
			store := identity.PGGrants{DB: grantDB{tbl}, SubjectKind: bgSubject.Kind, SubjectColumn: "ticket_id"}
			return identity.NewBreakGlassWith(clk, log, store), tbl, tbl
		}},
	}
}

// TestBreakGlassStoresOpenValidation runs the TestBreakGlassOpenValidation cases on both stores.
func TestBreakGlassStoresOpenValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*identity.BreakGlassRequest)
		ok   bool
	}{
		{"valid", nil, true},
		{"max duration", func(r *identity.BreakGlassRequest) { r.Duration = identity.BreakGlassMax }, true},
		{"over max", func(r *identity.BreakGlassRequest) { r.Duration = identity.BreakGlassMax + time.Second }, false},
		{"zero duration", func(r *identity.BreakGlassRequest) { r.Duration = 0 }, false},
		{"agent", func(r *identity.BreakGlassRequest) {
			r.By = identity.Principal{Kind: registry.PrincipalAgent, RunID: "r1", ActingFor: "u1"}
		}, false},
		{"guest", func(r *identity.BreakGlassRequest) {
			r.By = identity.Principal{Kind: registry.PrincipalGuest, UserID: "g1"}
		}, false},
		{"no reason", func(r *identity.BreakGlassRequest) { r.Reason = " " }, false},
		{"no explanation", func(r *identity.BreakGlassRequest) { r.Explanation = "" }, false},
		{"no subject", func(r *identity.BreakGlassRequest) { r.Subject = identity.Subject{} }, false},
	}
	for _, st := range bgStores() {
		for _, tc := range cases {
			t.Run(st.name+"/"+tc.name, func(t *testing.T) {
				log := auditfake.New()
				bg, tx, tbl := st.new(clockfake.New(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), time.UTC), log)
				_, err := bg.Open(context.Background(), tx, bgRequest(tc.mut))
				if tc.ok != (err == nil) {
					t.Fatalf("err = %v, want ok=%v", err, tc.ok)
				}
				if !tc.ok && !errors.Is(err, identity.ErrInvalidBreakGlass) {
					t.Fatalf("err = %v, want ErrInvalidBreakGlass", err)
				}
				want := 0
				if tc.ok {
					want = 1
				}
				if n := len(log.Events()); n != want {
					t.Fatalf("audit events = %d, want %d", n, want)
				}
				if tbl != nil && len(tbl.rows) != want {
					t.Fatalf("rows = %d, want %d", len(tbl.rows), want)
				}
			})
		}
	}
}

// TestBreakGlassStoresTimeBoxAndExpiry runs the TestBreakGlassTimeBoxAndExpiry cases on both stores.
func TestBreakGlassStoresTimeBoxAndExpiry(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		advance     time.Duration
		active      bool
		wantExpired int
	}{
		{"just opened", 0, true, 0},
		{"one second before end", 2*time.Hour - time.Second, true, 0},
		{"at end", 2 * time.Hour, false, 1},
		{"long after", 30 * time.Hour, false, 1},
	}
	for _, st := range bgStores() {
		for _, tc := range cases {
			t.Run(st.name+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				clk := clockfake.New(start, time.UTC)
				log := auditfake.New("grant_id", "starts_at", "ends_at", "user_id")
				bg, tx, _ := st.new(clk, log)
				g, err := bg.Open(ctx, tx, bgRequest(nil))
				if err != nil {
					t.Fatal(err)
				}
				if !g.EndsAt.Equal(start.Add(2*time.Hour)) || g.ID == "" {
					t.Fatalf("grant = %+v", g)
				}
				ev := log.Events()[0]
				if ev.Action != identity.ActionBreakGlassOpen || ev.ActorID != "u1" || ev.Subject != bgSubject || ev.Detail["grant_id"] != g.ID {
					t.Fatalf("open event = %+v", ev)
				}
				clk.Advance(tc.advance)
				if _, ok := bg.Active(ctx, "u1", bgSubject); ok != tc.active {
					t.Fatalf("Active = %v, want %v", ok, tc.active)
				}
				if _, ok := bg.Active(ctx, "u2", bgSubject); ok {
					t.Fatal("grant leaked to another user")
				}
				n, err := bg.Expire(ctx, tx)
				if err != nil || n != tc.wantExpired {
					t.Fatalf("Expire = %d, %v; want %d", n, err, tc.wantExpired)
				}
				if n, _ := bg.Expire(ctx, tx); n != 0 {
					t.Fatalf("second Expire = %d, want 0", n)
				}
				if len(log.Events()) != 1+tc.wantExpired {
					t.Fatalf("audit events = %d", len(log.Events()))
				}
				gs := bg.Grants(ctx)
				if len(gs) != 1 || gs[0].Expired != (tc.wantExpired == 1) || gs[0].Reason != "urgent" {
					t.Fatalf("Grants = %+v", gs)
				}
				if err := log.Verify(ctx, 1, 1); err != nil {
					t.Fatalf("chain: %v", err)
				}
			})
		}
	}
}

// TestPGGrantsGuards: the 8h cap and subject kind are checked by the store itself, writes need
// the caller's transaction, and read errors fail closed without leaking the grant's free text.
func TestPGGrantsGuards(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	noAudit := func(identity.BreakGlassGrant) error { return nil }
	grant := func(d time.Duration, kind string) identity.BreakGlassGrant {
		return identity.BreakGlassGrant{UserID: "u1", Subject: identity.Subject{Kind: kind, ID: "t1"},
			Reason: "secret-reason", Explanation: "secret-explanation", StartsAt: start, EndsAt: start.Add(d)}
	}
	boom := errors.New("connection reset")
	cases := []struct {
		name string
		run  func(s identity.PGGrants, tbl *grantTable) error
		want error
	}{
		{"over 8h refused by store", func(s identity.PGGrants, tbl *grantTable) error {
			_, err := s.Insert(ctx, tbl, grant(identity.BreakGlassMax+time.Minute, bgSubject.Kind), noAudit)
			return err
		}, identity.ErrInvalidBreakGlass},
		{"8h accepted", func(s identity.PGGrants, tbl *grantTable) error {
			_, err := s.Insert(ctx, tbl, grant(identity.BreakGlassMax, bgSubject.Kind), noAudit)
			return err
		}, nil},
		{"other subject kind refused", func(s identity.PGGrants, tbl *grantTable) error {
			_, err := s.Insert(ctx, tbl, grant(time.Hour, "other.kind"), noAudit)
			return err
		}, identity.ErrInvalidBreakGlass},
		{"insert without tx", func(s identity.PGGrants, _ *grantTable) error {
			_, err := s.Insert(ctx, nil, grant(time.Hour, bgSubject.Kind), noAudit)
			return err
		}, identity.ErrNoTx},
		{"expire without tx", func(s identity.PGGrants, _ *grantTable) error {
			_, err := s.ExpireDue(ctx, nil, start, noAudit)
			return err
		}, identity.ErrNoTx},
		{"audit failure fails insert", func(s identity.PGGrants, tbl *grantTable) error {
			_, err := s.Insert(ctx, tbl, grant(time.Hour, bgSubject.Kind), func(identity.BreakGlassGrant) error { return boom })
			return err
		}, boom},
		{"no subject column", func(_ identity.PGGrants, tbl *grantTable) error {
			_, err := identity.PGGrants{DB: grantDB{tbl}, SubjectKind: bgSubject.Kind}.List(ctx)
			return err
		}, identity.ErrNoSubjectColumn},
		{"list error wrapped", func(s identity.PGGrants, tbl *grantTable) error {
			tbl.fail = boom
			_, err := s.List(ctx)
			return err
		}, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := &grantTable{log: auditfake.New()}
			s := identity.PGGrants{DB: grantDB{tbl}, SubjectKind: bgSubject.Kind, SubjectColumn: "ticket_id"}
			err := tc.run(s, tbl)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret-") {
				t.Fatalf("error leaks grant text: %v", err)
			}
		})
	}

	t.Run("read error fails closed and logs no grant text", func(t *testing.T) {
		var logs bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })
		clk := clockfake.New(start, time.UTC)
		log := auditfake.New()
		tbl := &grantTable{log: log}
		bg := identity.NewBreakGlassWith(clk, log, identity.PGGrants{DB: grantDB{tbl}, SubjectKind: bgSubject.Kind, SubjectColumn: "ticket_id"})
		if _, err := bg.Open(ctx, tbl, bgRequest(func(r *identity.BreakGlassRequest) { r.Reason = "secret-reason" })); err != nil {
			t.Fatal(err)
		}
		tbl.fail = boom
		if _, ok := bg.Active(ctx, "u1", bgSubject); ok {
			t.Fatal("Active = true on a store error")
		}
		if gs := bg.Grants(ctx); gs != nil {
			t.Fatalf("Grants = %v on a store error", gs)
		}
		if logs.Len() == 0 || strings.Contains(logs.String(), "secret-") {
			t.Fatalf("logs = %q", logs.String())
		}
	})
}

// PORT-26 expired_at: ExpireDue marks each due grant in audit.break_glass, so a
// second run does not audit it again, whatever the audit masker keeps of the detail.
func TestPGGrantsExpiredAt(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		allow       []string // audit detail keys the masker keeps; none drops grant_id
		advance     time.Duration
		noMark      bool
		wantExpired int
		wantErr     error
		wantEvents  int // after both runs
	}{
		{"expiring sets expired_at", []string{"grant_id"}, 2 * time.Hour, false, 1, nil, 2},
		{"masker drops grant_id: still audited once", nil, 3 * time.Hour, false, 1, nil, 2},
		{"not yet due: no mark, no audit", []string{"grant_id"}, time.Hour, false, 0, nil, 1},
		{"mark fails: expiry fails closed", []string{"grant_id"}, 2 * time.Hour, true, 0, identity.ErrGrantNotMarked, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(start, time.UTC)
			log := auditfake.New(tc.allow...)
			tbl := &grantTable{log: log, noMark: tc.noMark}
			bg := identity.NewBreakGlassWith(clk, log, identity.PGGrants{DB: grantDB{tbl}, SubjectKind: bgSubject.Kind, SubjectColumn: "ticket_id"})
			if _, err := bg.Open(ctx, tbl, bgRequest(nil)); err != nil {
				t.Fatal(err)
			}
			clk.Advance(tc.advance)
			n, err := bg.Expire(ctx, tbl)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Expire err = %v, want %v", err, tc.wantErr)
			}
			if err == nil && n != tc.wantExpired {
				t.Fatalf("Expire = %d, want %d", n, tc.wantExpired)
			}
			got := tbl.rows[0].expiredAt
			if want := tc.wantExpired == 1; (got != nil) != want || (want && !got.Equal(start.Add(tc.advance))) {
				t.Fatalf("expired_at = %v, want set=%v at %v", got, want, start.Add(tc.advance))
			}
			if tc.wantErr != nil {
				// The caller's transaction rolls back with the error; the fake keeps the event,
				// so only check the grant stayed unmarked.
				return
			}
			if n, err := bg.Expire(ctx, tbl); err != nil || n != 0 {
				t.Fatalf("second Expire = %d, %v; want 0", n, err)
			}
			if len(log.Events()) != tc.wantEvents {
				t.Fatalf("audit events = %d, want %d", len(log.Events()), tc.wantEvents)
			}
			if gs := bg.Grants(ctx); len(gs) != 1 || gs[0].Expired != (tc.wantExpired == 1) {
				t.Fatalf("Grants = %+v", gs)
			}
		})
	}
}
