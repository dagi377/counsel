package withdraw

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeRow scans fixed values into the destinations, or returns err.
type fakeRow struct {
	vals []any
	err  error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("fake row: scan %d columns into %d", len(r.vals), len(dest))
	}
	for i, d := range dest {
		dv := reflect.ValueOf(d).Elem()
		if r.vals[i] == nil {
			dv.Set(reflect.Zero(dv.Type()))
			continue
		}
		v := reflect.ValueOf(r.vals[i])
		if dv.Kind() == reflect.Pointer && v.Kind() != reflect.Pointer {
			p := reflect.New(v.Type())
			p.Elem().Set(v)
			v = p
		}
		dv.Set(v)
	}
	return nil
}

// fakeRows is a one-pass result set over fakeRow values.
type fakeRows struct {
	pgx.Rows
	rows []fakeRow
	i    int
}

func (r *fakeRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *fakeRows) Scan(dest ...any) error { return r.rows[r.i-1].Scan(dest...) }
func (r *fakeRows) Err() error             { return nil }
func (r *fakeRows) Close()                 {}

// fakeTx answers queries by the first registered SQL fragment the statement contains, and
// records every Exec.
type fakeTx struct {
	pgx.Tx
	rows  map[string][]fakeRow // SQL fragment -> rows
	order []string             // fragments, most specific first
	execs []fakeExec
}

type fakeExec struct {
	sql  string
	args []any
}

func (t *fakeTx) match(sql string) []fakeRow {
	for _, k := range t.order {
		if strings.Contains(sql, k) {
			return t.rows[k]
		}
	}
	return nil
}

func (t *fakeTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	rs := t.match(sql)
	if len(rs) == 0 {
		return fakeRow{err: pgx.ErrNoRows}
	}
	return rs[0]
}

func (t *fakeTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	return &fakeRows{rows: t.match(sql)}, nil
}

func (t *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, fakeExec{sql: sql, args: args})
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// lockRow is the lockWithdrawalSQL row for w1 in status, authorised at authAt and expiring at
// expires (nil for none), read at dbNow.
func lockRow(status string, authAt, expires any, dbNow time.Time) fakeRow {
	return fakeRow{vals: []any{"w1", "led-1", "acct-1", "M1", int64(4000), "Vendor Inc", "cheque", "", "disbursement",
		"prep", status, "gate-1", "lawyer1", "", authAt, expires, dbNow, ""}}
}

// rule: ARC-I-012
// PGStore.Lock reads the stored expiry and fails closed once it has passed, or when an
// authorised row has none.
func TestARC_I_012_PGLockFailsClosedOnExpiry(t *testing.T) {
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	authAt := now.Add(-24 * time.Hour)
	tests := []struct {
		name       string
		status     string
		expires    any
		want       error
		wantExpiry time.Time
	}{
		{"authorised, unexpired", "authorized", authAt.Add(AuthorizationTTL), nil, authAt.Add(AuthorizationTTL)},
		{"authorised, expires exactly now", "authorized", now, ErrAuthorizationExpired, time.Time{}},
		{"authorised, expired", "authorized", now.Add(-time.Minute), ErrAuthorizationExpired, time.Time{}},
		{"authorised, no expiry recorded", "authorized", nil, ErrAuthorizationExpired, time.Time{}},
		{"requested, no expiry needed", "pending_authorization", nil, nil, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := any(authAt)
			if tt.status != "authorized" {
				auth = nil
			}
			tx := &fakeTx{rows: map[string][]fakeRow{"trust.withdrawal_request": {lockRow(tt.status, auth, tt.expires, now)}},
				order: []string{"trust.withdrawal_request"}}
			h, err := PGStore{}.Lock(context.Background(), tx, "w1")
			if !errors.Is(err, tt.want) {
				t.Fatalf("lock = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				if !errors.Is(err, ErrNotAuthorized) {
					t.Fatalf("lock = %v, want it to match ErrNotAuthorized", err)
				}
				return
			}
			if !h.ExpiresAt.Equal(tt.wantExpiry) {
				t.Fatalf("ExpiresAt = %v, want %v", h.ExpiresAt, tt.wantExpiry)
			}
		})
	}
}

// rule: ARC-I-012
// MarkAuthorized stores when the authorisation was given and when it expires; ReleaseExpired
// cancels expired (and expiry-less) authorisations so their holds drop.
func TestARC_I_012_PGAuthorizationExpiryStored(t *testing.T) {
	at := time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		at      time.Time
		wantErr error
	}{
		{"expiry is at plus the TTL", at, nil},
		{"no time fails closed", time.Time{}, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{}
			err := PGStore{}.MarkAuthorized(context.Background(), tx, "w1", "lawyer1", "gate-1", tt.at)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("mark = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(tx.execs) != 0 {
					t.Fatal("refused authorisation wrote")
				}
				return
			}
			if len(tx.execs) != 1 || !strings.Contains(tx.execs[0].sql, "expires_at = $5") {
				t.Fatalf("execs = %+v", tx.execs)
			}
			args := tx.execs[0].args
			if args[3] != tt.at || args[4] != tt.at.Add(AuthorizationTTL) {
				t.Fatalf("authorized_at, expires_at = %v, %v", args[3], args[4])
			}
		})
	}
	tx := &fakeTx{}
	n, err := PGStore{}.ReleaseExpired(context.Background(), tx, at)
	if err != nil || n != 1 {
		t.Fatalf("release = %d, %v", n, err)
	}
	if sql := tx.execs[0].sql; !strings.Contains(sql, "status = 'cancelled'") || !strings.Contains(sql, "expires_at IS NULL OR expires_at <= $1") {
		t.Fatalf("release sql = %s", sql)
	}
}

// rule: BIL-L-038
// The Form 9A signer is the signer on the signature record, never the uploader; with no
// signature record there is no Form 9A.
func TestBIL_L_038_Form9ASignerFromSignatureRecord(t *testing.T) {
	uploadedAt := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	signedAt := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	binding := []fakeRow{{vals: []any{"w1", int64(4000)}}}
	// Every query on docs.document alone answers with the uploader, as the old reader used.
	uploader := []fakeRow{{vals: []any{"clerk1", uploadedAt}}}
	tests := []struct {
		name       string
		signature  []fakeRow
		want       error
		wantSigner string
		wantAt     time.Time
	}{
		{"signer is the signature record's", []fakeRow{{vals: []any{"lawyer1", signedAt}}}, nil, "lawyer1", signedAt},
		{"no signature record fails closed", nil, ErrNotFound, "", time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{rows: map[string][]fakeRow{"form_9a_signature": tt.signature, "trust.withdrawal_request": binding,
				"docs.document": uploader}, order: []string{"form_9a_signature", "trust.withdrawal_request", "docs.document"}}
			a, err := PGEFTRecords{}.EFTAuthorization(context.Background(), tx, "9a-1")
			if !errors.Is(err, tt.want) {
				t.Fatalf("record = %+v, %v; want %v", a, err, tt.want)
			}
			if a.SignedBy != tt.wantSigner || !a.SignedAt.Equal(tt.wantAt) {
				t.Fatalf("signed by %q at %v, want %q at %v", a.SignedBy, a.SignedAt, tt.wantSigner, tt.wantAt)
			}
			if tt.want == nil && a.WithdrawalID != "w1" {
				t.Fatalf("record = %+v", a)
			}
		})
	}
}

// rule: ARC-I-019
// PGApprovals reads only an approved trust.withdrawal gate bound to the withdrawal.
func TestARC_I_019_PGApprovedPayload(t *testing.T) {
	tests := []struct {
		name string
		rows []fakeRow
		want error
	}{
		{"approved payload", []fakeRow{{vals: []any{[]byte(`{"withdrawal_id":"w1"}`)}}}, nil},
		{"no approved gate", nil, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{rows: map[string][]fakeRow{"gate.approval_request": tt.rows}, order: []string{"gate.approval_request"}}
			p, err := PGApprovals{}.ApprovedPayload(context.Background(), tx, "gate-1", "w1")
			if !errors.Is(err, tt.want) {
				t.Fatalf("payload = %s, %v; want %v", p, err, tt.want)
			}
		})
	}
	for _, frag := range []string{"status = 'approved'", "payload_id = $3::uuid", "gate_code = $2", "payload_jcs IS NOT NULL"} {
		if !strings.Contains(approvedPayloadSQL, frag) {
			t.Errorf("approvedPayloadSQL lacks %q", frag)
		}
	}
}
