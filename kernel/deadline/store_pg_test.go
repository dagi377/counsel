package deadline_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// pgRow is one cal.deadline row held by tableTx.
type pgRow struct {
	kind, subject, pack, version, rule, status, updatedBy string
	trigger, due                                          time.Time
}

// pgConfirm is one cal.deadline_confirmation row held by tableTx.
type pgConfirm struct {
	deadline, user string
	date           time.Time
}

// tableTx answers PGBook's statements from in-memory cal.deadline and
// cal.deadline_confirmation tables. Values arrive only as arguments, never in the SQL text;
// an unknown statement fails the call.
type tableTx struct {
	pgx.Tx
	rows     map[string]*pgRow
	log      []pgConfirm
	n        int
	updates  int
	failNext error
	// afterSelect runs once after the repack select, to model a concurrent change.
	afterSelect func()
}

func newTableTx() *tableTx { return &tableTx{rows: map[string]*pgRow{}} }

func (f *tableTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "INSERT INTO cal.deadline"):
		if len(args) != 7 {
			return pgRowResult{err: fmt.Errorf("insert: %d args", len(args))}
		}
		f.n++
		id := fmt.Sprintf("00000000-0000-0000-0000-%012d", f.n)
		f.rows[id] = &pgRow{kind: args[0].(string), subject: args[1].(string), pack: args[2].(string),
			version: args[3].(string), rule: args[4].(string), trigger: args[5].(time.Time),
			due: args[6].(time.Time), status: "proposed"}
		return pgRowResult{vals: []any{id}}
	case strings.Contains(sql, "FOR UPDATE") && strings.Contains(sql, "WHERE id = $1"):
		r := f.rows[args[0].(string)]
		if r == nil {
			return pgRowResult{err: pgx.ErrNoRows}
		}
		return pgRowResult{vals: []any{r.status, r.due}}
	case strings.Contains(sql, "FROM cal.deadline d"):
		id := args[0].(string)
		r := f.rows[id]
		if r == nil {
			return pgRowResult{err: pgx.ErrNoRows}
		}
		var by *string
		for _, c := range f.log {
			if c.deadline == id {
				u := c.user
				by = &u
				break
			}
		}
		trig := r.trigger
		return pgRowResult{vals: []any{&r.kind, &r.subject, &r.pack, &r.version, &r.rule, &trig, r.due, r.status, by}}
	}
	return pgRowResult{err: fmt.Errorf("tableTx: unexpected QueryRow %q", sql)}
}

func (f *tableTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM cal.deadline_confirmation"):
		var out [][]any
		for _, c := range f.log {
			if c.deadline == args[0].(string) {
				d := c.date
				out = append(out, []any{c.user, &d})
			}
		}
		return &pgRows{rows: out}, nil
	case strings.Contains(sql, "pack_code = $1") && strings.Contains(sql, "FOR UPDATE"):
		var ids []string
		for id, r := range f.rows {
			if r.pack == args[0].(string) && r.version != args[1].(string) && r.status == "proposed" {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		var out [][]any
		for _, id := range ids {
			out = append(out, []any{id, f.rows[id].rule, f.rows[id].trigger})
		}
		if f.afterSelect != nil {
			f.afterSelect()
			f.afterSelect = nil
		}
		return &pgRows{rows: out}, nil
	}
	return nil, fmt.Errorf("tableTx: unexpected Query %q", sql)
}

func (f *tableTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := f.failNext; err != nil {
		f.failNext = nil
		return pgconn.CommandTag{}, err
	}
	switch {
	case strings.Contains(sql, "SET status = 'confirmed'"):
		r := f.rows[args[0].(string)]
		if r == nil || r.status != "proposed" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		r.status, r.updatedBy = "confirmed", args[1].(string)
		f.updates++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.Contains(sql, "INSERT INTO cal.deadline_confirmation"):
		for _, c := range f.log {
			if c.deadline == args[0].(string) && c.user == args[1].(string) {
				return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505", ConstraintName: "deadline_confirmation_once"}
			}
		}
		f.log = append(f.log, pgConfirm{deadline: args[0].(string), user: args[1].(string), date: args[2].(time.Time)})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(sql, "SET pack_version = $2"):
		r := f.rows[args[0].(string)]
		// The guard in the statement: a confirmed row is never re-computed.
		if r == nil || r.status != "proposed" {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		r.version, r.due = args[1].(string), args[2].(time.Time)
		f.updates++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("tableTx: unexpected Exec %q", sql)
}

type pgRowResult struct {
	vals []any
	err  error
}

func (r pgRowResult) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return assign(r.vals, dest)
}

type pgRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *pgRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *pgRows) Scan(dest ...any) error { return assign(r.rows[r.i-1], dest) }
func (r *pgRows) Err() error             { return nil }
func (r *pgRows) Close()                 {}

func assign(vals, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("scan: %d values into %d targets", len(vals), len(dest))
	}
	for i, v := range vals {
		target := reflect.ValueOf(dest[i]).Elem()
		if v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil()) {
			target.Set(reflect.Zero(target.Type()))
			continue
		}
		target.Set(reflect.ValueOf(v))
	}
	return nil
}

// txRunner runs fn on one tableTx, like kernel/db.DB.InTx.
type txRunner struct{ tx *tableTx }

func (r txRunner) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

const subjectID = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"

func pgProposal(t *testing.T, p deadline.Pack, rule string, trigger deadline.Date) deadline.Proposed {
	t.Helper()
	pr := proposal(t, p, rule, trigger)
	pr.Subject = deadline.Subject{Kind: "ticket", ID: subjectID}
	return pr
}

func newPG() (*deadline.PGBook, *tableTx) {
	tx := newTableTx()
	b := deadline.NewPGBook(txRunner{tx})
	b.Events = outboxfake.New()
	return b, tx
}

func TestPGBookSaveGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	b, tx := newPG()
	want := pgProposal(t, helpdesk(t), "auto_close", date(2026, time.March, 2))
	id, err := b.Save(ctx, tx, want)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	r := tx.rows[string(id)]
	if r == nil || r.kind != "ticket" || r.subject != subjectID || r.pack != want.PackID || r.rule != "auto_close" {
		t.Fatalf("stored row = %+v", r)
	}
	got, err := b.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != deadline.StateProposed || !reflect.DeepEqual(got.Proposed, want) {
		t.Fatalf("Get = %+v, want proposed %+v", got, want)
	}
}

func TestPGBookSaveRefusesIncompleteProposal(t *testing.T) {
	ctx := context.Background()
	full := pgProposal(t, helpdesk(t), "auto_close", date(2026, time.March, 2))
	tests := []struct {
		name string
		edit func(*deadline.Proposed)
	}{
		{"no pack", func(p *deadline.Proposed) { p.PackID = "" }},
		{"no version", func(p *deadline.Proposed) { p.PackVersion = "" }},
		{"no rule", func(p *deadline.Proposed) { p.RuleID = "" }},
		{"no subject kind", func(p *deadline.Proposed) { p.Subject.Kind = "" }},
		{"subject id not a uuid", func(p *deadline.Proposed) { p.Subject.ID = "t-1" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, tx := newPG()
			p := full
			tc.edit(&p)
			if _, err := b.Save(ctx, tx, p); !errors.Is(err, deadline.ErrInvalidPack) {
				t.Fatalf("Save err = %v, want ErrInvalidPack", err)
			}
			if len(tx.rows) != 0 {
				t.Fatalf("rows written: %d", len(tx.rows))
			}
		})
	}
}

// The confirm_test.go cases against scripted rows.
func TestPGBookConfirmNeedsPerson(t *testing.T) {
	ctx := context.Background()
	p := helpdesk(t)
	tests := []struct {
		name      string
		by        deadline.Principal
		twice     bool
		wantErr   error
		wantState deadline.State
	}{
		{"person confirms", person, false, nil, deadline.StateConfirmed},
		{"agent refused", agent, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"guest refused", deadline.Principal{Kind: registry.PrincipalGuest, UserID: "g-1"}, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"anonymous user refused", deadline.Principal{Kind: registry.PrincipalUser}, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"unknown kind refused", deadline.Principal{UserID: "u-1"}, false, deadline.ErrAgentCannotConfirm, deadline.StateProposed},
		{"second confirm by same person refused", person, true, deadline.ErrAlreadyConfirmed, deadline.StateConfirmed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, tx := newPG()
			id, err := b.Save(ctx, tx, pgProposal(t, p, "auto_close", date(2026, time.March, 2)))
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if tc.twice {
				if err := b.Confirm(ctx, tx, id, tc.by); err != nil {
					t.Fatalf("first Confirm: %v", err)
				}
			}
			err = b.Confirm(ctx, tx, id, tc.by)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("Confirm err = %v, want %v", err, tc.wantErr)
			}
			d, err := b.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if d.State != tc.wantState {
				t.Fatalf("state = %s, want %s", d.State, tc.wantState)
			}
			wantLog := 0
			if tc.wantState == deadline.StateConfirmed {
				wantLog = 1
				if d.ConfirmedBy != person.UserID {
					t.Fatalf("ConfirmedBy = %q, want %q", d.ConfirmedBy, person.UserID)
				}
			}
			got, err := b.Confirmations(ctx, tx, id)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != wantLog {
				t.Fatalf("confirmation log = %v, want %d entries", got, wantLog)
			}
		})
	}
}

func TestPGBookConfirmUnknownDeadline(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		id   deadline.DeadlineID
	}{
		{"no such row", "00000000-0000-0000-0000-000000000099"},
		{"not a uuid", "missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, tx := newPG()
			if err := b.Confirm(ctx, tx, tc.id, person); !errors.Is(err, deadline.ErrNotFound) {
				t.Fatalf("Confirm err = %v, want ErrNotFound", err)
			}
			if _, err := b.Get(ctx, tc.id); !errors.Is(err, deadline.ErrNotFound) {
				t.Fatalf("Get err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestPGBookConfirmationLogKeepsDue(t *testing.T) {
	ctx := context.Background()
	p := helpdesk(t)
	b, tx := newPG()
	id, err := b.Save(ctx, tx, pgProposal(t, p, "auto_close", date(2026, time.March, 2)))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Confirm(ctx, tx, id, person); err != nil {
		t.Fatal(err)
	}
	want := []deadline.Confirmation{{DeadlineID: id, UserID: "u-1", Due: date(2026, time.March, 16)}}
	got, err := b.Confirmations(ctx, tx, id)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %v, %v; want %v", got, err, want)
	}
	// A new pack version does not touch the confirmed deadline or its log entry.
	v2 := p
	v2.Version = "2"
	v2.Rules = []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 21, Roll: deadline.RollNone}}
	if _, err := b.Repack(ctx, tx, v2); err != nil {
		t.Fatal(err)
	}
	if again, _ := b.Confirmations(ctx, tx, id); !reflect.DeepEqual(again, want) {
		t.Fatalf("log changed by repack: %v", again)
	}
}

// The confirm_test.go repack cases against scripted rows.
func TestPGBookRepackRecomputesUnconfirmedOnly(t *testing.T) {
	ctx := context.Background()
	v1 := helpdesk(t)
	trigger := date(2026, time.March, 2)
	v2 := v1
	v2.Version = "2"
	v2.Rules = []deadline.Rule{
		{ID: "first_response", Mode: deadline.Business, Days: 2, Roll: deadline.RollForward},
		{ID: "auto_close", Mode: deadline.Calendar, Days: 21, Roll: deadline.RollNone},
	}
	other := deadline.Pack{ID: "other", Version: "1", Rules: []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 3}}}

	tests := []struct {
		name        string
		pack        deadline.Pack
		rule        string
		confirm     bool
		repack      deadline.Pack
		wantChanged bool
		wantDue     deadline.Date
		wantVersion string
	}{
		{"unconfirmed calendar recomputed", v1, "auto_close", false, v2, true, date(2026, time.March, 23), "2"},
		{"unconfirmed business recomputed", v1, "first_response", false, v2, true, date(2026, time.March, 4), "2"},
		{"confirmed kept", v1, "auto_close", true, v2, false, date(2026, time.March, 16), "1"},
		{"same version is a no-op", v1, "auto_close", false, v1, false, date(2026, time.March, 16), "1"},
		{"other pack untouched", other, "auto_close", false, v2, false, date(2026, time.March, 5), "1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, tx := newPG()
			id, err := b.Save(ctx, tx, pgProposal(t, tc.pack, tc.rule, trigger))
			if err != nil {
				t.Fatal(err)
			}
			if tc.confirm {
				if err := b.Confirm(ctx, tx, id, person); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := b.Repack(ctx, tx, tc.repack)
			if err != nil {
				t.Fatalf("Repack: %v", err)
			}
			if got := len(changed) == 1 && changed[0] == id; got != tc.wantChanged {
				t.Fatalf("changed = %v, want changed %t", changed, tc.wantChanged)
			}
			d, err := b.Get(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if d.Proposed.Due != tc.wantDue || d.Proposed.PackVersion != tc.wantVersion {
				t.Fatalf("due %s v%s, want %s v%s", d.Proposed.Due, d.Proposed.PackVersion, tc.wantDue, tc.wantVersion)
			}
			wantState := deadline.StateProposed
			if tc.confirm {
				wantState = deadline.StateConfirmed
			}
			if d.State != wantState {
				t.Fatalf("state = %s, want %s", d.State, wantState)
			}
		})
	}
}

// Repack never overwrites a confirmed deadline: confirmed rows are not selected, and a row
// confirmed between the select and the update is left alone because the update itself is
// guarded by status.
func TestPGBookRepackNeverOverwritesConfirmed(t *testing.T) {
	ctx := context.Background()
	v1 := helpdesk(t)
	v2 := v1
	v2.Version = "2"
	v2.Rules = []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 21}, {ID: "first_response", Mode: deadline.Business, Days: 2}}
	tests := []struct {
		name          string
		confirmBefore bool
		confirmRace   bool
	}{
		{"confirmed before repack", true, false},
		{"confirmed after select, before update", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, tx := newPG()
			id, err := b.Save(ctx, tx, pgProposal(t, v1, "auto_close", date(2026, time.March, 2)))
			if err != nil {
				t.Fatal(err)
			}
			if tc.confirmBefore {
				if err := b.Confirm(ctx, tx, id, person); err != nil {
					t.Fatal(err)
				}
			}
			if tc.confirmRace {
				tx.afterSelect = func() { tx.rows[string(id)].status = "confirmed" }
			}
			changed, err := b.Repack(ctx, tx, v2)
			if err != nil || len(changed) != 0 {
				t.Fatalf("Repack = %v, %v; want nothing changed", changed, err)
			}
			r := tx.rows[string(id)]
			if r.version != "1" || !r.due.Equal(time.Date(2026, time.March, 16, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("confirmed deadline overwritten: %+v", r)
			}
		})
	}
}

func TestPGBookRepackFailsWithoutPartialChange(t *testing.T) {
	ctx := context.Background()
	v1 := helpdesk(t)
	trigger := date(2026, time.March, 2)
	v2 := v1
	v2.Version = "2"
	v2.Rules = []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 21}}
	tests := []struct {
		name string
		pack deadline.Pack
		want error
	}{
		{"rule removed", v2, deadline.ErrUnknownRule},
		{"no version", deadline.Pack{ID: v1.ID}, deadline.ErrInvalidPack},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, tx := newPG()
			keep, err := b.Save(ctx, tx, pgProposal(t, v1, "auto_close", trigger))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Save(ctx, tx, pgProposal(t, v1, "first_response", trigger)); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Repack(ctx, tx, tc.pack); !errors.Is(err, tc.want) {
				t.Fatalf("Repack err = %v, want %v", err, tc.want)
			}
			if tx.updates != 0 {
				t.Fatalf("%d rows updated on failed repack", tx.updates)
			}
			d, err := b.Get(ctx, keep)
			if err != nil {
				t.Fatal(err)
			}
			if d.Proposed.PackVersion != "1" || d.Proposed.Due != date(2026, time.March, 16) {
				t.Fatalf("deadline changed on failed repack: %+v", d.Proposed)
			}
		})
	}
}

func TestPGBookComputeAndLoadPack(t *testing.T) {
	ctx := context.Background()
	p := helpdesk(t)
	trigger := date(2026, time.March, 2)
	subject := deadline.Subject{Kind: "ticket", ID: subjectID}
	tests := []struct {
		name    string
		load    deadline.Pack
		pack    string
		rule    string
		want    []deadline.Proposed
		wantErr error
	}{
		{"computes loaded rule", p, p.ID, "auto_close",
			[]deadline.Proposed{{PackID: p.ID, PackVersion: "1", RuleID: "auto_close", Trigger: trigger, Due: date(2026, time.March, 16), Subject: subject}}, nil},
		{"unknown pack", p, "nope", "auto_close", nil, deadline.ErrUnknownPack},
		{"unknown rule", p, p.ID, "nope", nil, deadline.ErrUnknownRule},
		{"invalid pack refused", deadline.Pack{ID: "x"}, "x", "auto_close", nil, deadline.ErrInvalidPack},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := newPG()
			if err := b.LoadPack(ctx, tc.load); err != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("LoadPack err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			got, err := b.Compute(ctx, tc.pack, tc.rule, trigger, subject)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("Compute err = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Compute = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPGBookConfirmStoreErrorIsWrapped(t *testing.T) {
	ctx := context.Background()
	b, tx := newPG()
	id, err := b.Save(ctx, tx, pgProposal(t, helpdesk(t), "auto_close", date(2026, time.March, 2)))
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("connection reset")
	tx.failNext = boom
	if err := b.Confirm(ctx, tx, id, person); !errors.Is(err, boom) {
		t.Fatalf("Confirm err = %v, want %v", err, boom)
	}
}

// rule: ARC-I-035
// Repack publishes rulepack.published in its tx, so F-DL-03 recomputes the dependants; with no
// emitter it writes nothing.
func TestPGBookRepackPublishesPackEvent(t *testing.T) {
	ctx := context.Background()
	v1 := deadline.Pack{ID: "helpdesk_sla", Version: "1", Rules: []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 14}}}
	v2 := deadline.Pack{ID: "helpdesk_sla", Version: "2", Rules: []deadline.Rule{{ID: "auto_close", Mode: deadline.Calendar, Days: 21}}}
	tests := []struct {
		name      string
		noEvents  bool
		wantErr   error
		wantEvent bool
	}{
		{name: "event with the recomputed count", wantEvent: true},
		{name: "no emitter writes nothing", noEvents: true, wantErr: deadline.ErrInvalidPack},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, tx := newPG()
			ev := outboxfake.New()
			b.Events = ev
			if err := b.LoadPack(ctx, v1); err != nil {
				t.Fatal(err)
			}
			id, err := b.Save(ctx, tx, pgProposal(t, v1, "auto_close", date(2026, time.March, 2)))
			if err != nil {
				t.Fatal(err)
			}
			if tt.noEvents {
				b.Events = nil
			}
			_, err = b.Repack(ctx, tx, v2)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Repack err = %v, want %v", err, tt.wantErr)
			}
			msgs := ev.Messages()
			if !tt.wantEvent {
				if len(msgs) != 0 || tx.rows[string(id)].version != "1" {
					t.Fatalf("refused Repack emitted %d events or changed the row", len(msgs))
				}
				return
			}
			if len(msgs) != 1 || msgs[0].Type != deadline.EventPackPublished ||
				msgs[0].Subject != (deadline.Subject{Kind: deadline.SubjectRulePack, ID: "helpdesk_sla"}) ||
				string(msgs[0].Payload) != `{"pack_id":"helpdesk_sla","version":"2","recomputed":1}` {
				t.Fatalf("events = %+v", msgs)
			}
		})
	}
}
