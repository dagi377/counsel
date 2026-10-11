package sync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
)

// subCall is one statement the scripted tx saw.
type subCall struct {
	sql  string
	args []any
}

// subTx answers the three store statements from scripted rows keyed by id, and records
// every statement with its args. err, when set, fails every statement.
type subTx struct {
	pgx.Tx
	rows     map[string]*Subscription
	err      error
	affected int64 // when >= 0, overrides the rows an UPDATE reports
	calls    []subCall
}

func newSubTx(rows ...Subscription) *subTx {
	t := &subTx{rows: map[string]*Subscription{}, affected: -1}
	for i := range rows {
		r := rows[i]
		t.rows[r.ID] = &r
	}
	return t
}

func (t *subTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.calls = append(t.calls, subCall{sql, args})
	if t.err != nil {
		return nil, t.err
	}
	if sql != expiringSQL {
		return nil, fmt.Errorf("unexpected Query %q", sql)
	}
	before := args[0].(time.Time)
	var out []Subscription
	for _, r := range t.rows {
		if !r.ExpiresAt.After(before) {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ExpiresAt.Equal(out[j].ExpiresAt) {
			return out[i].ExpiresAt.Before(out[j].ExpiresAt)
		}
		return out[i].ID < out[j].ID
	})
	return &subRows{rows: out}, nil
}

func (t *subTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.calls = append(t.calls, subCall{sql, args})
	if t.err != nil {
		return pgconn.CommandTag{}, t.err
	}
	r, ok := t.rows[args[0].(string)]
	n := int64(0)
	if ok {
		n = 1
		switch sql {
		case markRenewedSQL:
			at := args[2].(time.Time)
			r.ExpiresAt, r.LastRenewedAt, r.LastError = args[1].(time.Time), &at, ""
		case markFailedSQL:
			r.LastError = args[1].(string)
		default:
			return pgconn.CommandTag{}, fmt.Errorf("unexpected Exec %q", sql)
		}
	}
	if t.affected >= 0 {
		n = t.affected
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

// subRows is a scripted pgx.Rows over Subscription values.
type subRows struct {
	pgx.Rows
	rows []Subscription
	i    int
}

func (r *subRows) Next() bool { r.i++; return r.i <= len(r.rows) }
func (r *subRows) Scan(dest ...any) error {
	s := r.rows[r.i-1]
	*dest[0].(*string) = s.ID
	*dest[1].(*string) = s.Source
	*dest[2].(*string) = s.RemoteID
	*dest[3].(*[]string) = s.Events
	*dest[4].(*time.Time) = s.ExpiresAt
	*dest[5].(**time.Time) = s.LastRenewedAt
	*dest[6].(*string) = s.LastError
	return nil
}
func (r *subRows) Err() error { return nil }
func (r *subRows) Close()     {}

// subDB runs fn on its scripted tx.
type subDB struct {
	db.DB
	tx *subTx
}

func (d subDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

// The memStore cases of renew_test.go, with PGSubscriptions over scripted rows.
//
// rule: MIG-042
// rule: MIG-041
// rule: ARC-I-049
func TestMIG_042_PGSubscriptionRenewal(t *testing.T) {
	expires := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	newExp := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		now         time.Time
		fail        bool
		wantPatches int
		wantExp     time.Time
		wantRenewed bool
		wantErr     bool
		wantEvents  int
		wantAlerts  int
	}{
		{"outside window untouched", time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), false, 0, expires, false, false, 0, 0},
		{"inside window renewed", time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC), false, 1, newExp, true, false, 0, 0},
		{"failure recorded, no alert", time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC), true, 1, expires, false, true, 1, 0},
		{"failure near expiry alerts", time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC), true, 1, expires, false, true, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := newSubTx(Subscription{ID: "s1", Source: "clio", RemoteID: "r1", Events: []string{"matter"}, ExpiresAt: expires})
			src := &fakeSource{fail: tc.fail}
			note := &recNotifier{}
			job := &RenewJob{Store: NewPGSubscriptions(subDB{tx: tx}), Source: src, Notifier: note, Clock: clockfake.New(tc.now, time.UTC)}
			if _, err := job.Run(context.Background()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			row := tx.rows["s1"]
			if len(src.patches) != tc.wantPatches {
				t.Errorf("patches = %d, want %d", len(src.patches), tc.wantPatches)
			}
			if !row.ExpiresAt.Equal(tc.wantExp) {
				t.Errorf("expires_at = %v, want %v", row.ExpiresAt, tc.wantExp)
			}
			if (row.LastRenewedAt != nil) != tc.wantRenewed || (tc.wantRenewed && !row.LastRenewedAt.Equal(tc.now)) {
				t.Errorf("last_renewed_at = %v, want set=%v at %v", row.LastRenewedAt, tc.wantRenewed, tc.now)
			}
			if (row.LastError != "") != tc.wantErr {
				t.Errorf("last_error = %q, want set=%v", row.LastError, tc.wantErr)
			}
			if len(note.events) != tc.wantEvents {
				t.Errorf("events = %v, want %d", note.events, tc.wantEvents)
			}
			if len(note.alerts) != tc.wantAlerts {
				t.Errorf("alerts = %d, want %d", len(note.alerts), tc.wantAlerts)
			}
		})
	}
}

// rule: MIG-042
func TestMIG_042_PGHourlyRetryAfterFailure(t *testing.T) {
	expires := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	clk := clockfake.New(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), time.UTC)
	tx := newSubTx(Subscription{ID: "s1", Source: "clio", RemoteID: "r1", ExpiresAt: expires})
	src := &fakeSource{fail: true}
	job := &RenewJob{Store: NewPGSubscriptions(subDB{tx: tx}), Source: src, Notifier: &recNotifier{}, Clock: clk}
	steps := []struct {
		fail                 bool
		wantRenewed, wantErr bool
	}{
		{true, false, true},
		{true, false, true},
		{false, true, false},
	}
	for i, st := range steps {
		src.fail = st.fail
		res, err := job.Run(context.Background())
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if (res.Renewed == 1) != st.wantRenewed || (tx.rows["s1"].LastError != "") != st.wantErr {
			t.Errorf("run %d: res=%+v last_error=%q", i, res, tx.rows["s1"].LastError)
		}
		clk.Advance(RenewInterval)
	}
	if len(src.patches) != 3 {
		t.Errorf("patches = %d, want 3", len(src.patches))
	}
}

func TestPGSubscriptionsStatements(t *testing.T) {
	before := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	exp := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		run  func(s *PGSubscriptions) error
		want subCall
	}{
		{"expiring", func(s *PGSubscriptions) error { _, err := s.Expiring(context.Background(), before); return err },
			subCall{expiringSQL, []any{before}}},
		{"renewed", func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "s1", exp, at) },
			subCall{markRenewedSQL, []any{"s1", exp, at}}},
		{"failed", func(s *PGSubscriptions) error { return s.MarkFailed(context.Background(), "s1", "source answered 500") },
			subCall{markFailedSQL, []any{"s1", "source answered 500"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newSubTx(Subscription{ID: "s1", ExpiresAt: before})
			if err := tt.run(NewPGSubscriptions(subDB{tx: tx})); err != nil {
				t.Fatal(err)
			}
			if len(tx.calls) != 1 || !reflect.DeepEqual(tx.calls[0], tt.want) {
				t.Fatalf("calls = %#v, want %#v", tx.calls, tt.want)
			}
		})
	}
	if strings.Contains(markFailedSQL, "expires_at") {
		t.Error("MarkFailed changes expires_at")
	}
}

func TestPGSubscriptionsFailClosed(t *testing.T) {
	boom := errors.New("connection reset")
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		tx       *subTx
		db       bool
		run      func(s *PGSubscriptions) error
		err      error
		wantCall bool
	}{
		{"expiring db error", &subTx{err: boom, affected: -1}, true,
			func(s *PGSubscriptions) error { _, err := s.Expiring(context.Background(), now); return err }, boom, true},
		{"expiring zero time", newSubTx(), true,
			func(s *PGSubscriptions) error { _, err := s.Expiring(context.Background(), time.Time{}); return err }, nil, false},
		{"renewed unknown id", newSubTx(), true,
			func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "s9", now, now) }, ErrSubscriptionNotFound, true},
		{"renewed malformed id", &subTx{err: &pgconn.PgError{Code: "22P02"}, affected: -1}, true,
			func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "x", now, now) }, ErrSubscriptionNotFound, true},
		{"renewed zero expiry", newSubTx(Subscription{ID: "s1"}), true,
			func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "s1", time.Time{}, now) }, nil, false},
		{"renewed zero renewal time", newSubTx(Subscription{ID: "s1"}), true,
			func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "s1", now, time.Time{}) }, nil, false},
		{"renewed no id", newSubTx(), true,
			func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "", now, now) }, nil, false},
		{"renewed two rows", &subTx{rows: map[string]*Subscription{"s1": {ID: "s1"}}, affected: 2}, true,
			func(s *PGSubscriptions) error { return s.MarkRenewed(context.Background(), "s1", now, now) }, nil, true},
		{"failed unknown id", newSubTx(), true,
			func(s *PGSubscriptions) error { return s.MarkFailed(context.Background(), "s9", "x") }, ErrSubscriptionNotFound, true},
		{"failed db error", &subTx{err: boom, affected: -1}, true,
			func(s *PGSubscriptions) error { return s.MarkFailed(context.Background(), "s1", "x") }, boom, true},
		{"failed no id", newSubTx(), true,
			func(s *PGSubscriptions) error { return s.MarkFailed(context.Background(), "", "x") }, nil, false},
		{"no database", nil, false,
			func(s *PGSubscriptions) error { return s.MarkFailed(context.Background(), "s1", "x") }, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PGSubscriptions{}
			if tt.db {
				s = NewPGSubscriptions(subDB{tx: tt.tx})
			}
			err := tt.run(s)
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.tx != nil && (len(tt.tx.calls) > 0) != tt.wantCall {
				t.Fatalf("calls = %v, want a call: %v", tt.tx.calls, tt.wantCall)
			}
		})
	}
}

// No token or secret value is stored: MarkFailed redacts and truncates last_error.
func TestPGSubscriptionsLastErrorRedacted(t *testing.T) {
	secrets := []string{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlLXZhbHVl", "s3cr3t-pa55", "AbCdEf0123456789AbCdEf0123", "hunter2"}
	tests := []struct {
		name    string
		in      string
		keep    []string
		secrets []string
	}{
		{"bearer header", "PATCH failed: Authorization: Bearer " + secrets[0] + " rejected", []string{"PATCH failed", "rejected"}, secrets[:1]},
		{"access token query", "GET https://app.example.com/api/v4/webhooks?access_token=" + secrets[1] + "&x=1: 401", []string{"401", "&x=1"}, secrets[1:2]},
		{"json refresh token", `body {"refresh_token": "` + secrets[1] + `"}`, []string{"refresh_token"}, secrets[1:2]},
		{"client secret pair", "client_secret=" + secrets[3] + " invalid", []string{"invalid"}, secrets[3:]},
		{"unlabelled opaque token", "token " + secrets[2] + " expired", []string{"expired"}, secrets[2:3]},
		{"url user info", "dial https://bot:" + secrets[3] + "@hooks.example.com: refused", []string{"hooks.example.com", "refused"}, secrets[3:]},
		{"plain error kept", "source answered 500", []string{"source answered 500"}, nil},
		{"empty still records failure", "  ", []string{"renewal failed"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newSubTx(Subscription{ID: "s1"})
			if err := NewPGSubscriptions(subDB{tx: tx}).MarkFailed(context.Background(), "s1", tt.in); err != nil {
				t.Fatal(err)
			}
			stored := tx.rows["s1"].LastError
			if stored != tx.calls[0].args[1] {
				t.Fatalf("stored %q, sent %q", stored, tx.calls[0].args[1])
			}
			for _, s := range tt.secrets {
				if strings.Contains(stored, s) {
					t.Fatalf("last_error %q keeps secret %q", stored, s)
				}
			}
			for _, k := range tt.keep {
				if !strings.Contains(stored, k) {
					t.Fatalf("last_error %q lost %q", stored, k)
				}
			}
		})
	}
	t.Run("truncated on a rune boundary", func(t *testing.T) {
		long := strings.Repeat("é rror ", 200)
		got := RedactLastError(long)
		if n := utf8.RuneCountInString(got); n != MaxLastErrorLen {
			t.Fatalf("len = %d runes, want %d", n, MaxLastErrorLen)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("truncated error is not valid UTF-8: %q", got)
		}
	})
	t.Run("invalid UTF-8 replaced", func(t *testing.T) {
		if got := RedactLastError("bad \xff byte"); !utf8.ValidString(got) {
			t.Fatalf("got %q", got)
		}
	})
}
