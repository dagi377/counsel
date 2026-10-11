package webhook

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// expiryRow is one platform.webhook row as PGExpiry sees it.
type expiryRow struct {
	status    string
	expiresAt *time.Time
	noticed   *time.Time // expiry_noticed_for
}

// expiryTx answers PGExpiry's three statements from rows, applying each statement's WHERE
// clause, and records the arguments. Any other statement fails; methods PGExpiry does not
// call panic through the nil embedded Tx.
type expiryTx struct {
	pgx.Tx
	rows map[string]*expiryRow
	fail error // returned by every statement when set
	args map[string][]any
}

func (f *expiryTx) record(sql string, args []any) {
	if f.args == nil {
		f.args = map[string][]any{}
	}
	f.args[sql] = args
}

func (f *expiryTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.record(sql, args)
	if f.fail != nil {
		return nil, f.fail
	}
	if sql != sqlExpiring {
		return nil, fmt.Errorf("expiryTx: unexpected Query %q", sql)
	}
	before := args[0].(time.Time)
	var out expiryRows
	for id, r := range f.rows {
		if r.status == "active" && r.expiresAt != nil && !r.expiresAt.After(before) {
			out.rows = append(out.rows, []any{id, *r.expiresAt})
		}
	}
	sort.Slice(out.rows, func(i, j int) bool { return out.rows[i][1].(time.Time).Before(out.rows[j][1].(time.Time)) })
	return &out, nil
}

func (f *expiryTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.record(sql, args)
	if f.fail != nil {
		return expiryRow1{err: f.fail}
	}
	if sql != sqlClaimNotice {
		return expiryRow1{err: fmt.Errorf("expiryTx: unexpected QueryRow %q", sql)}
	}
	id, exp, at := args[0].(string), args[1].(time.Time), args[2].(time.Time)
	r, ok := f.rows[id]
	if !ok || r.expiresAt == nil || !r.expiresAt.Equal(exp) || (r.noticed != nil && !r.noticed.Before(at)) {
		return expiryRow1{err: pgx.ErrNoRows}
	}
	r.noticed = &at
	return expiryRow1{id: id}
}

func (f *expiryTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.record(sql, args)
	if f.fail != nil {
		return pgconn.CommandTag{}, f.fail
	}
	if sql != sqlSetExpiry {
		return pgconn.CommandTag{}, fmt.Errorf("expiryTx: unexpected Exec %q", sql)
	}
	r, ok := f.rows[args[0].(string)]
	if !ok {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	r.expiresAt, r.noticed = args[1].(*time.Time), nil
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type expiryRow1 struct {
	id  string
	err error
}

func (r expiryRow1) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.id
	return nil
}

type expiryRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *expiryRows) Next() bool { r.i++; return r.i <= len(r.rows) }
func (r *expiryRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.rows[r.i-1][0].(string)
	*dest[1].(*time.Time) = r.rows[r.i-1][1].(time.Time)
	return nil
}
func (r *expiryRows) Err() error { return nil }
func (r *expiryRows) Close()     {}

// txDB is a db.DB whose InTx always hands fn the same tx.
type txDB struct{ tx pgx.Tx }

func (d txDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (txDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (txDB) Ping(context.Context) error                     { return nil }
func (txDB) Close()                                         {}

func newPGExpiry(rows map[string]*expiryRow) (PGExpiry, *expiryTx) {
	tx := &expiryTx{rows: rows}
	return PGExpiry{DB: txDB{tx}}, tx
}

// rule: ARC-I-049
func TestARC_I_049_PGExpiryEndpointExpiringEvent(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	tests := []struct {
		name    string
		row     expiryRow
		runs    int
		want    int
		wantLed string
	}{
		{"expires in 14 days: one event", expiryRow{status: "active", expiresAt: ptr(now.Add(14 * day))}, 3, 1, "336h0m0s"},
		{"expires in 15 days: none yet", expiryRow{status: "active", expiresAt: ptr(now.Add(15 * day))}, 1, 0, ""},
		{"expires in 1 day: one-day notice", expiryRow{status: "active", expiresAt: ptr(now.Add(day))}, 2, 1, "24h0m0s"},
		{"tenant-owned: never", expiryRow{status: "active"}, 1, 0, ""},
		{"already expired: none", expiryRow{status: "active", expiresAt: ptr(now.Add(-time.Hour))}, 1, 0, ""},
		{"disabled: none", expiryRow{status: "disabled", expiresAt: ptr(now.Add(day))}, 1, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := tc.row
			store, _ := newPGExpiry(map[string]*expiryRow{"e1": &row})
			var got []ExpiringNotice
			emit := func(_ context.Context, _ string, n ExpiringNotice) error { got = append(got, n); return nil }
			for range tc.runs {
				if _, err := NotifyExpiring(context.Background(), store, emit, now); err != nil {
					t.Fatal(err)
				}
			}
			if len(got) != tc.want {
				t.Fatalf("events = %d, want %d", len(got), tc.want)
			}
			if tc.want > 0 && got[0].Lead != tc.wantLed {
				t.Errorf("lead = %q, want %q", got[0].Lead, tc.wantLed)
			}
		})
	}
}

// rule: ARC-I-049
func TestARC_I_049_PGExpiryClaimNotice(t *testing.T) {
	exp := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	renewed := exp.Add(30 * 24 * time.Hour)
	day := 24 * time.Hour
	type step struct {
		set   bool       // SetExpiry(id, to) instead of a claim
		to    *time.Time // SetExpiry target
		id    string
		exp   time.Time
		lead  time.Duration
		first bool
		err   error
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"claim twice: second false", []step{
			{id: "e1", exp: exp, lead: 14 * day, first: true},
			{id: "e1", exp: exp, lead: 14 * day, first: false},
		}},
		{"longer then shorter lead: both claimed once", []step{
			{id: "e1", exp: exp, lead: 14 * day, first: true},
			{id: "e1", exp: exp, lead: day, first: true},
			{id: "e1", exp: exp, lead: day, first: false},
			{id: "e1", exp: exp, lead: 14 * day, first: false},
		}},
		{"SetExpiry resets the claim", []step{
			{id: "e1", exp: exp, lead: 14 * day, first: true},
			{set: true, id: "e1", to: &renewed},
			{id: "e1", exp: renewed, lead: 14 * day, first: true},
			{id: "e1", exp: renewed, lead: 14 * day, first: false},
		}},
		{"stale expiry claims nothing", []step{
			{set: true, id: "e1", to: &renewed},
			{id: "e1", exp: exp, lead: 14 * day, first: false},
		}},
		{"never expires after SetExpiry nil", []step{
			{set: true, id: "e1", to: nil},
			{id: "e1", exp: exp, lead: day, first: false},
		}},
		{"unknown endpoint", []step{
			{id: "nope", exp: exp, lead: day, first: false},
			{set: true, id: "nope", to: &renewed, err: ErrUnknownEndpoint},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := exp
			store, tx := newPGExpiry(map[string]*expiryRow{"e1": {status: "active", expiresAt: &e}})
			ctx := context.Background()
			for i, s := range tc.steps {
				if s.set {
					if err := store.SetExpiry(ctx, s.id, s.to); !errors.Is(err, s.err) {
						t.Fatalf("step %d SetExpiry err = %v, want %v", i, err, s.err)
					}
					continue
				}
				first, err := store.ClaimNotice(ctx, s.id, s.exp, s.lead)
				if !errors.Is(err, s.err) || first != s.first {
					t.Fatalf("step %d ClaimNotice = %v, %v; want %v, %v", i, first, err, s.first, s.err)
				}
				if a := tx.args[sqlClaimNotice]; !a[2].(time.Time).Equal(s.exp.Add(-s.lead)) {
					t.Fatalf("step %d claimed instant %v, want expiry minus lead", i, a[2])
				}
			}
		})
	}
}

func TestPGExpiryErrorsAreWrapped(t *testing.T) {
	boom := errors.New("connection reset")
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		call func(PGExpiry) error
	}{
		{"expiring", func(s PGExpiry) error { _, err := s.Expiring(context.Background(), now); return err }},
		{"claim", func(s PGExpiry) error {
			_, err := s.ClaimNotice(context.Background(), "e1", now, time.Hour)
			return err
		}},
		{"set expiry", func(s PGExpiry) error { return s.SetExpiry(context.Background(), "e1", &now) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store, tx := newPGExpiry(map[string]*expiryRow{})
			tx.fail = boom
			if err := tc.call(store); !errors.Is(err, boom) || err.Error() == boom.Error() {
				t.Fatalf("err = %v, want %v wrapped with context", err, boom)
			}
		})
	}
	store, _ := newPGExpiry(map[string]*expiryRow{})
	if _, err := store.ClaimNotice(context.Background(), "e1", now, 0); err == nil {
		t.Fatal("ClaimNotice with zero lead: want error")
	}
}
