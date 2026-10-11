package signatory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
)

// tableRow is one scripted trust.signatory row.
type tableRow struct {
	account string
	row
}

// tableDB plays trust.signatory for the three statements PGService runs: it filters and
// updates its scripted rows the way the SQL does.
type tableDB struct {
	db.DB
	rows []*tableRow
	err  error // returned by every statement when set
}

func (d *tableDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, tableTx{d: d})
}

type tableTx struct {
	pgx.Tx
	d *tableDB
}

func current(r *tableRow, today time.Time) bool { return r.ValidTo == nil || !r.ValidTo.Before(today) }

func (t tableTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if t.d.err != nil {
		return nil, t.d.err
	}
	account, today := args[0].(string), args[1].(time.Time)
	var out []row
	for _, r := range t.d.rows {
		if r.account != account || !current(r, today) {
			continue
		}
		if sql == canSignSQL && r.UserID != args[2].(string) {
			continue
		}
		out = append(out, r.row)
	}
	return &tableRows{rows: out, i: -1}, nil
}

func (t tableTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if t.d.err != nil {
		return pgconn.CommandTag{}, t.d.err
	}
	if sql != freezeSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected sql %q", sql)
	}
	user, today := args[0].(string), args[1].(time.Time)
	n := 0
	for _, r := range t.d.rows {
		if r.UserID == user && r.ValidTo == nil {
			end := today
			if r.ValidFrom.After(end) {
				end = r.ValidFrom
			}
			r.ValidTo = &end
			n++
		}
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n)), nil
}

type tableRows struct {
	pgx.Rows
	rows []row
	i    int
}

func (r *tableRows) Next() bool { r.i++; return r.i < len(r.rows) }
func (r *tableRows) Close()     {}
func (r *tableRows) Err() error { return nil }
func (r *tableRows) Scan(dest ...any) error {
	x := r.rows[r.i]
	*dest[0].(*string) = x.UserID
	*dest[1].(*bool) = x.Licensee
	*dest[2].(*bool) = x.Bonded
	*dest[3].(**time.Time) = x.BondExpiry
	*dest[4].(*time.Time) = x.ValidFrom
	*dest[5].(**time.Time) = x.ValidTo
	return nil
}

var (
	day0      = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	yesterday = day0.AddDate(0, 0, -1)
	tomorrow  = day0.AddDate(0, 0, 1)
)

func ptr(t time.Time) *time.Time { return &t }

// seededDB mirrors signatoryfake's seeded(): account "a1" signed by licensee "lic1".
func seededDB(extra ...*tableRow) *tableDB {
	rows := []*tableRow{{account: "a1", row: row{UserID: "lic1", Licensee: true, ValidFrom: yesterday}}}
	return &tableDB{rows: append(rows, extra...)}
}

func todayCtx(t *testing.T) context.Context {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return clock.WithClock(context.Background(), clockfake.New(time.Date(2026, 10, 11, 12, 0, 0, 0, loc), loc))
}

// The same cases as signatoryfake's TestFreezeAndRemove.
func TestPGServiceFreezeAndRemove(t *testing.T) {
	ctx := todayCtx(t)
	tests := []struct {
		name       string
		op         func(s *PGService, d *tableDB) error
		wantFrozen int
		wantLic1   bool
		wantList   int
		wantStatus Status
	}{
		{name: "freeze on leaving", op: func(s *PGService, _ *tableDB) error {
			_, err := s.Freeze(ctx, "lic1")
			return err
		}, wantFrozen: 1, wantLic1: false, wantList: 1, wantStatus: StatusFrozen},
		{name: "remove after adding another", op: func(_ *PGService, d *tableDB) error {
			// What an approved change writes: lic1 ended yesterday, lic2 granted today.
			d.rows[0].ValidTo = ptr(yesterday)
			d.rows = append(d.rows, &tableRow{account: "a1", row: row{UserID: "lic2", Licensee: true, ValidFrom: day0}})
			return nil
		}, wantLic1: false, wantList: 1, wantStatus: StatusActive},
		{name: "freeze unknown is no-op", op: func(s *PGService, _ *tableDB) error {
			n, err := s.Freeze(ctx, "nobody")
			if n != 0 {
				return fmt.Errorf("frozen = %d, want 0", n)
			}
			return err
		}, wantLic1: true, wantList: 1, wantStatus: StatusActive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := seededDB()
			s := NewPGService(d)
			if err := tt.op(s, d); err != nil {
				t.Fatal(err)
			}
			if ok, err := s.CanSign(ctx, "a1", "lic1"); err != nil || ok != tt.wantLic1 {
				t.Fatalf("lic1 CanSign = %v, %v; want %v", ok, err, tt.wantLic1)
			}
			got, err := s.List(ctx, "a1")
			if err != nil || len(got) != tt.wantList {
				t.Fatalf("List = %d, %v; want %d", len(got), err, tt.wantList)
			}
			if got[0].Status != tt.wantStatus || got[0].AccountID != "a1" {
				t.Fatalf("List[0] = %+v, want status %s", got[0], tt.wantStatus)
			}
		})
	}
}

func TestPGServiceCanSign(t *testing.T) {
	ctx := todayCtx(t)
	tests := []struct {
		name string
		row  row
		want bool
		kind Kind
	}{
		{name: "licensee open", row: row{Licensee: true, ValidFrom: yesterday}, want: true, kind: Licensee},
		{name: "licensee starts today", row: row{Licensee: true, ValidFrom: day0}, want: true, kind: Licensee},
		{name: "licensee starts tomorrow", row: row{Licensee: true, ValidFrom: tomorrow}, kind: Licensee},
		{name: "licensee ends tomorrow", row: row{Licensee: true, ValidFrom: yesterday, ValidTo: ptr(tomorrow)}, want: true, kind: Licensee},
		{name: "licensee frozen today", row: row{Licensee: true, ValidFrom: yesterday, ValidTo: ptr(day0)}, kind: Licensee},
		{name: "licensee ended yesterday", row: row{Licensee: true, ValidFrom: yesterday, ValidTo: ptr(yesterday)}, kind: Licensee},
		{name: "bonded no expiry", row: row{Bonded: true, ValidFrom: yesterday}, want: true, kind: BondedNonLicensee},
		{name: "bond expires today", row: row{Bonded: true, BondExpiry: ptr(day0), ValidFrom: yesterday}, want: true, kind: BondedNonLicensee},
		{name: "bond expired", row: row{Bonded: true, BondExpiry: ptr(yesterday), ValidFrom: yesterday}, kind: BondedNonLicensee},
		{name: "neither licensee nor bond", row: row{ValidFrom: yesterday}, kind: BondedNonLicensee},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.row.UserID = "u1"
			s := NewPGService(&tableDB{rows: []*tableRow{{account: "a1", row: tt.row}}})
			got, err := s.CanSign(ctx, "a1", "u1")
			if err != nil || got != tt.want {
				t.Fatalf("CanSign = %v, %v; want %v", got, err, tt.want)
			}
			if ok, _ := s.CanSign(ctx, "a2", "u1"); ok {
				t.Fatal("signs on another account")
			}
			if ok, _ := s.CanSign(ctx, "a1", "u2"); ok {
				t.Fatal("another user signs")
			}
			list, err := s.List(ctx, "a1")
			if err != nil {
				t.Fatal(err)
			}
			if tt.row.ValidTo != nil && tt.row.ValidTo.Before(day0) {
				if len(list) != 0 {
					t.Fatalf("ended row listed: %+v", list)
				}
				return
			}
			wantStatus := StatusFrozen
			if tt.want {
				wantStatus = StatusActive
			}
			if len(list) != 1 || list[0].Status != wantStatus || list[0].Kind != tt.kind {
				t.Fatalf("List = %+v, want one %s %s", list, wantStatus, tt.kind)
			}
		})
	}
}

func TestPGServiceFreezeFutureGrant(t *testing.T) {
	ctx := todayCtx(t)
	d := &tableDB{rows: []*tableRow{
		{account: "a1", row: row{UserID: "u1", Licensee: true, ValidFrom: yesterday}},
		{account: "a2", row: row{UserID: "u1", Licensee: true, ValidFrom: tomorrow}},
		{account: "a3", row: row{UserID: "u1", Licensee: true, ValidFrom: yesterday, ValidTo: ptr(yesterday)}},
	}}
	n, err := NewPGService(d).Freeze(ctx, "u1")
	if err != nil || n != 2 {
		t.Fatalf("Freeze = %d, %v; want 2", n, err)
	}
	for _, r := range d.rows[:2] {
		if r.ValidTo == nil || r.ValidTo.Before(r.ValidFrom) {
			t.Fatalf("%s: valid_to = %v, valid_from %v", r.account, r.ValidTo, r.ValidFrom)
		}
	}
	if !strings.Contains(freezeSQL, "GREATEST(valid_from") || !strings.Contains(freezeSQL, "valid_to IS NULL") {
		t.Fatalf("freezeSQL = %q", freezeSQL)
	}
}

func TestPGServiceErrors(t *testing.T) {
	ctx := todayCtx(t)
	boom := errors.New("connection reset")
	bad := &pgconn.PgError{Code: pgInvalidTextRepresentation}
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "read error wrapped", err: boom, wantErr: boom},
		{name: "malformed id is unknown", err: bad},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewPGService(&tableDB{err: tt.err})
			if _, err := s.List(ctx, "a1"); !errors.Is(err, tt.wantErr) {
				t.Errorf("List err = %v, want %v", err, tt.wantErr)
			}
			if _, err := s.CanSign(ctx, "a1", "u1"); !errors.Is(err, tt.wantErr) {
				t.Errorf("CanSign err = %v, want %v", err, tt.wantErr)
			}
			if _, err := s.Freeze(ctx, "u1"); !errors.Is(err, tt.wantErr) {
				t.Errorf("Freeze err = %v, want %v", err, tt.wantErr)
			}
		})
	}
	s := NewPGService(&tableDB{})
	if _, err := s.Propose(ctx, Change{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Propose err = %v", err)
	}
	if _, err := s.Approve(ctx, "c1", Approver{UserID: "u2"}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Approve err = %v", err)
	}
}
