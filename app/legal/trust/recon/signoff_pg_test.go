package recon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/clock"
)

// scriptRow scans vals into dest in order, or returns err.
type scriptRow struct {
	vals []any
	err  error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, v := range r.vals {
		switch d := dest[i].(type) {
		case *bool:
			*d = v.(bool)
		case *string:
			*d = v.(string)
		case *int64:
			*d = v.(int64)
		}
	}
	return nil
}

// lockedConn answers the Locked query: month March 2026 of acct-1 is signed.
type lockedConn struct{ args []any }

func (c *lockedConn) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	c.args = args
	m := args[1].(time.Time)
	return scriptRow{vals: []any{args[0] == "acct-1" && m.Year() == 2026 && m.Month() == time.March && m.Day() == 1}}
}

// rule: BIL-I-025
func TestBIL_I_025_PGSignOffLocksPosting(t *testing.T) {
	d := func(m time.Month, day int) clock.Date { return clock.Date{Year: 2026, Month: m, Day: day} }
	tests := []struct {
		name    string
		account string
		date    clock.Date
		want    error
	}{
		{"signed month refuses posting dated in it", "acct-1", d(time.March, 15), recon.ErrPeriodLocked},
		{"signed month refuses posting on its last day", "acct-1", d(time.March, 31), recon.ErrPeriodLocked},
		{"correction posts in the current month", "acct-1", d(time.April, 2), nil},
		{"other account is not locked", "acct-2", d(time.March, 15), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := recon.NewPGSignOff(&lockedConn{})
			if err := recon.CheckPostingDate(bylaw9Ctx(t, true), s, tt.account, tt.date); !errors.Is(err, tt.want) {
				t.Fatalf("posting %s: %v, want %v", tt.date, err, tt.want)
			}
		})
	}
	if _, err := recon.NewPGSignOff(scriptConn{err: &pgconn.PgError{Code: "22P02"}}).Locked(context.Background(), "x", recon.Period{Year: 2026, Month: 3}); err != nil {
		t.Fatalf("Locked on a non-uuid account: %v, want not locked", err)
	}
	boom := errors.New("connection reset")
	if _, err := recon.NewPGSignOff(scriptConn{err: boom}).Locked(context.Background(), "acct-1", recon.Period{Year: 2026, Month: 3}); !errors.Is(err, boom) {
		t.Fatalf("Locked on a failing database: %v, want %v", err, boom)
	}
}

type scriptConn struct{ err error }

func (c scriptConn) QueryRow(context.Context, string, ...any) pgx.Row { return scriptRow{err: c.err} }

// signTx scripts the sign-off: the load query returns load, the update affects updated rows.
type signTx struct {
	pgx.Tx
	load    scriptRow
	updated int64
	exec    []any
}

func (t *signTx) QueryRow(context.Context, string, ...any) pgx.Row { return t.load }

func (t *signTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	t.exec = args
	if t.updated == 1 {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

// rule: BIL-I-025
func TestBIL_I_025_PGSignOffSign(t *testing.T) {
	at := time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)
	sum := []byte{0xab, 0xcd}
	rec := recon.SignOffRecord{ReconciliationID: "r1", By: recon.Approver{UserID: "approver"}, At: at, PackageSHA256: sum, ApprovalID: "g1"}
	row := func(status string, adjusted, journal, clients int64) scriptRow {
		return scriptRow{vals: []any{"preparer", status, adjusted, journal, clients}}
	}
	with := func(f func(*recon.SignOffRecord)) recon.SignOffRecord {
		r := rec
		f(&r)
		return r
	}
	tests := []struct {
		name     string
		tx       *signTx
		rec      recon.SignOffRecord
		want     error
		wantExec bool
	}{
		{"balanced ready recon is signed", &signTx{load: row("ready", 100, 100, 100), updated: 1}, rec, nil, true},
		{"unbalanced recon", &signTx{load: row("ready", 100, 100, 99), updated: 1}, rec, recon.ErrUnbalanced, false},
		{"bank does not match book", &signTx{load: row("draft", 101, 100, 100), updated: 1}, rec, recon.ErrUnbalanced, false},
		{"preparer cannot sign", &signTx{load: row("ready", 100, 100, 100), updated: 1}, with(func(r *recon.SignOffRecord) { r.By.UserID = "preparer" }), recon.ErrSelfApproval, false},
		{"agent cannot sign", &signTx{load: row("ready", 100, 100, 100), updated: 1}, with(func(r *recon.SignOffRecord) { r.By.IsAgent = true }), recon.ErrAgentApproval, false},
		{"already signed", &signTx{load: row("signed", 100, 100, 100), updated: 1}, rec, recon.ErrInvalidState, false},
		{"imported month", &signTx{load: row("imported", 100, 100, 100), updated: 1}, rec, recon.ErrInvalidState, false},
		{"no package hash", &signTx{load: row("ready", 100, 100, 100), updated: 1}, with(func(r *recon.SignOffRecord) { r.PackageSHA256 = nil }), recon.ErrInvalidState, false},
		{"no approval", &signTx{load: row("ready", 100, 100, 100), updated: 1}, with(func(r *recon.SignOffRecord) { r.ApprovalID = "" }), recon.ErrInvalidState, false},
		{"no signing time", &signTx{load: row("ready", 100, 100, 100), updated: 1}, with(func(r *recon.SignOffRecord) { r.At = time.Time{} }), recon.ErrInvalidState, false},
		{"unknown recon", &signTx{load: scriptRow{err: pgx.ErrNoRows}}, rec, recon.ErrNotFound, false},
		{"row changed under the update", &signTx{load: row("ready", 100, 100, 100), updated: 0}, rec, recon.ErrInvalidState, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := recon.NewPGSignOff(nil).Sign(context.Background(), tt.tx, tt.rec)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Sign = %v, want %v", err, tt.want)
			}
			if (tt.tx.exec != nil) != tt.wantExec {
				t.Fatalf("update ran = %v, want %v", tt.tx.exec != nil, tt.wantExec)
			}
			if tt.want == nil {
				got := tt.tx.exec
				if got[0] != "r1" || got[1] != "approver" || !got[2].(time.Time).Equal(at) || string(got[3].([]byte)) != string(sum) || got[4] != "g1" {
					t.Fatalf("update args = %v", got)
				}
			}
		})
	}
}
