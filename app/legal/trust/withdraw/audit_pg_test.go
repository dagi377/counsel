package withdraw

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	kaudit "github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
)

// failLog is an audit log whose Append always fails.
type failLog struct{ kaudit.Log }

func (failLog) Append(context.Context, pgx.Tx, kaudit.Event) error {
	return errors.New("audit store down")
}

func TestAuditLogRecord(t *testing.T) {
	tests := []struct {
		name    string
		log     kaudit.Log
		action  string
		id      string
		by      string
		wantErr error
		wantAny bool // any error at all (for errors without a sentinel)
		wantN   int
	}{
		{name: "solo exception", log: auditfake.New(), action: AuditSoloException, id: "w1", by: "lawyer1", wantN: 1},
		{name: "other action", log: auditfake.New(), action: "trust.withdrawal.executed", id: "w2", by: "lawyer2", wantN: 1},
		{name: "no log fails closed", log: nil, action: AuditSoloException, id: "w1", by: "lawyer1", wantErr: ErrNoAuditLog},
		{name: "empty action", log: auditfake.New(), action: "", id: "w1", by: "lawyer1", wantErr: kaudit.ErrInvalidEvent},
		{name: "empty withdrawal", log: auditfake.New(), action: AuditSoloException, id: "", by: "lawyer1", wantErr: kaudit.ErrInvalidEvent},
		{name: "empty actor", log: auditfake.New(), action: AuditSoloException, id: "w1", by: "", wantErr: kaudit.ErrInvalidEvent},
		{name: "audit error aborts", log: failLog{}, action: AuditSoloException, id: "w1", by: "lawyer1", wantAny: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAuditLog(tt.log)
			err := a.Record(context.Background(), nil, tt.action, tt.id, tt.by)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			case tt.wantAny:
				if err == nil || !strings.Contains(err.Error(), "audit store down") {
					t.Fatalf("err = %v, want wrapped audit error", err)
				}
			case err != nil:
				t.Fatalf("Record: %v", err)
			}
			f, ok := tt.log.(*auditfake.Fake)
			if !ok {
				return
			}
			ev := f.Events()
			if len(ev) != tt.wantN {
				t.Fatalf("events = %d, want %d", len(ev), tt.wantN)
			}
			if tt.wantN == 0 {
				return
			}
			e := ev[0]
			if e.Action != tt.action || e.ActorKind != "user" || e.ActorID != tt.by || e.ActingFor != "" {
				t.Fatalf("event = %+v", e)
			}
			if e.Subject.Kind != GateKind || e.Subject.ID != tt.id {
				t.Fatalf("subject = %+v, want %s/%s", e.Subject, GateKind, tt.id)
			}
		})
	}
}

func TestAuditLogNilReceiverFailsClosed(t *testing.T) {
	var a *AuditLog
	if err := a.Record(context.Background(), nil, AuditSoloException, "w1", "u1"); !errors.Is(err, ErrNoAuditLog) {
		t.Fatalf("err = %v, want ErrNoAuditLog", err)
	}
}

// auditTx is a scripted tx: it records every Exec and answers with err.
type auditTx struct {
	pgx.Tx
	sql  []string
	args [][]any
	err  error
}

func (t *auditTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.sql = append(t.sql, sql)
	t.args = append(t.args, args)
	return pgconn.NewCommandTag("INSERT 0 1"), t.err
}

// TestAuditLogWritesInCallerTx drives the production kernel log on a scripted tx: the one
// insert goes to the tx Record was given, and nothing in it is an amount or account number.
func TestAuditLogWritesInCallerTx(t *testing.T) {
	tests := []struct {
		name    string
		execErr error
		wantErr bool
	}{
		{name: "insert in tx"},
		{name: "insert error aborts", execErr: errors.New("conn reset"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &auditTx{err: tt.execErr}
			a := NewAuditLog(kaudit.New(nil, nil, kaudit.Options{}))
			err := a.Record(context.Background(), tx, AuditSoloException, "w1", "lawyer1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if len(tx.sql) != 1 || !strings.Contains(tx.sql[0], "INSERT INTO audit.audit_event") {
				t.Fatalf("sql = %v, want one audit insert in the caller's tx", tx.sql)
			}
			if !strings.Contains(fmt.Sprint(tx.args[0]...), GateKind) {
				t.Fatalf("args = %v, want subject kind %s", tx.args[0], GateKind)
			}
		})
	}
}

// TestAuditLogNoAmountsOrAccounts checks the event metadata never carries money or account
// numbers: the detail is empty and no field holds a value from the withdrawal it records.
func TestAuditLogNoAmountsOrAccounts(t *testing.T) {
	f := auditfake.New("amount_cents", "account_no", "trust_account_id") // allow, so nothing is masked away
	if err := NewAuditLog(f).Record(context.Background(), nil, AuditSoloException, "w1", "lawyer1"); err != nil {
		t.Fatal(err)
	}
	ev := f.Events()
	if len(ev) != 1 {
		t.Fatalf("events = %d", len(ev))
	}
	if len(ev[0].Detail) != 0 {
		t.Fatalf("detail = %v, want none (no amounts or account numbers)", ev[0].Detail)
	}
	for _, banned := range []string{"amount", "cents", "account", "125000"} {
		for _, v := range []string{ev[0].Action, ev[0].Subject.Kind, ev[0].Subject.ID, ev[0].ActorID} {
			if strings.Contains(v, banned) {
				t.Fatalf("event field %q carries %q", v, banned)
			}
		}
	}
}
