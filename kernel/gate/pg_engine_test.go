package gate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/db/dbfake"
)

// engineGate is one gate version held by engineTx.
type engineGate struct {
	kind      string
	status    Status
	requester string
	runID     string
	closedAt  *time.Time
	expiresAt time.Time
	jcs       []byte
	effectRan bool
}

// engineTx answers the engine's statements from an in-memory gate table, like the
// scripted-row fakeTx in kernel/ledger. Unknown statements fail the call.
type engineTx struct {
	pgx.Tx
	gates     map[string]*engineGate
	expireRun int
}

func (f *engineTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	id := ""
	if len(args) > 0 {
		id, _ = args[0].(string)
	}
	g := f.gates[id]
	switch {
	case strings.Contains(sql, "gate.expire_due()"):
		f.expireRun++
		return engineRow{vals: []any{0}}
	case g == nil && strings.Contains(sql, "WHERE r.id = $1"), g == nil && strings.Contains(sql, "WHERE id = $1"):
		return engineRow{err: pgx.ErrNoRows}
	case strings.Contains(sql, "SELECT 1 FROM gate.approval_request"):
		return engineRow{vals: []any{1}}
	case strings.Contains(sql, "SELECT id::text FROM gate.gate_decision"):
		return engineRow{vals: []any{"decision-" + id}}
	case strings.Contains(sql, "SELECT status, requested_by::text"):
		return engineRow{vals: []any{string(g.status), g.requester}}
	case strings.Contains(sql, "r.supersedes_id::text"):
		var run *string
		if g.runID != "" {
			run = &g.runID
		}
		sum := sha256.Sum256(g.jcs)
		return engineRow{vals: []any{g.kind, 1, (*string)(nil), "desk.refund_request", "s1", sum[:], g.jcs,
			g.requester, run, string(g.status), g.expiresAt.Add(-time.Hour), g.expiresAt,
			false, g.closedAt, g.effectRan}}
	case strings.Contains(sql, "SELECT r.gate_code, r.version, r.status"):
		sum := sha256.Sum256(g.jcs)
		return engineRow{vals: []any{g.kind, 1, string(g.status), sum[:], "desk.refund_request", "s1", g.requester,
			g.expiresAt, g.closedAt, g.jcs, sum[:], g.closedAt, g.effectRan}}
	}
	return engineRow{err: fmt.Errorf("engineTx: unexpected QueryRow %q", sql)}
}

func (f *engineTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "SELECT r.id::text FROM gate.approval_request r"):
		var rows [][]any
		for id, g := range f.gates {
			if g.status == StatusApproved && !g.effectRan && g.closedAt != nil && g.closedAt.Before(g.expiresAt) {
				rows = append(rows, []any{id})
			}
		}
		return &engineRows{rows: rows}, nil
	case strings.Contains(sql, "FROM gate.gate_decision"):
		return &engineRows{rows: [][]any{{"approver"}}}, nil
	}
	return nil, fmt.Errorf("engineTx: unexpected Query %q", sql)
}

func (f *engineTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(sql, "INSERT INTO gate.effect_execution"):
		g := f.gates[args[0].(string)]
		if g.effectRan {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		g.effectRan = true
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(sql, "UPDATE gate.effect_execution"), strings.Contains(sql, "set_config"):
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.Contains(sql, "SET status = 'withdrawn'"):
		g := f.gates[args[0].(string)]
		if g.status == StatusPending {
			g.status = StatusWithdrawn
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("engineTx: unexpected Exec %q", sql)
}

// Begin is the effect savepoint: the fake has no partial state to undo.
func (f *engineTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *engineTx) Commit(context.Context) error          { return nil }
func (f *engineTx) Rollback(context.Context) error        { return nil }

type engineRow struct {
	vals []any
	err  error
}

func (r engineRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return engineAssign(r.vals, dest)
}

type engineRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *engineRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *engineRows) Scan(dest ...any) error { return engineAssign(r.rows[r.i-1], dest) }
func (r *engineRows) Err() error             { return nil }
func (r *engineRows) Close()                 {}

func engineAssign(vals, dest []any) error {
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

// engineRunner runs fn on one engineTx, like db.DB.InTx.
type engineRunner struct{ tx *engineTx }

func (r engineRunner) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

func engineCtx(t *testing.T, now time.Time) context.Context {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return clock.WithClock(context.Background(), clockfake.New(now, loc))
}

func TestWithdrawPreparerOnlyAndOnlyPending(t *testing.T) {
	user := func(id string) Principal { return Principal{Kind: PrincipalUser, UserID: id} }
	tests := []struct {
		name       string
		status     Status
		by         Principal
		id         string
		want       error
		wantStatus Status
	}{
		{"preparer withdraws a pending gate", StatusPending, user("prep"), "g1", nil, StatusWithdrawn},
		{"agent acting for the preparer", StatusPending, Principal{Kind: PrincipalAgent, RunID: "r", ActingFor: "prep"}, "g1", nil, StatusWithdrawn},
		{"non-preparer refused", StatusPending, user("other"), "g1", ErrNotPreparer, StatusPending},
		{"agent acting for someone else refused", StatusPending, Principal{Kind: PrincipalAgent, RunID: "r", ActingFor: "other"}, "g1", ErrNotPreparer, StatusPending},
		{"guest refused", StatusPending, Principal{Kind: PrincipalGuest, UserID: "prep"}, "g1", ErrNotPreparer, StatusPending},
		{"approved gate refused", StatusApproved, user("prep"), "g1", ErrNotPending, StatusApproved},
		{"rejected gate refused", StatusRejected, user("prep"), "g1", ErrNotPending, StatusRejected},
		{"already withdrawn refused", StatusWithdrawn, user("prep"), "g1", ErrNotPending, StatusWithdrawn},
		{"missing gate", StatusPending, user("prep"), "nope", ErrNotFound, StatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &engineGate{kind: "desk.refund", status: tt.status, requester: "prep"}
			tx := &engineTx{gates: map[string]*engineGate{"g1": g}}
			err := NewPG().Withdraw(context.Background(), tx, GateID(tt.id), tt.by)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("Withdraw = %v, want %v", err, tt.want)
			}
			if g.status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", g.status, tt.wantStatus)
			}
		})
	}
}

func TestRunDueRunsEachEffectOnce(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	calls := map[string]int{}
	e := NewPG()
	for _, k := range []struct {
		slug    string
		cooling time.Duration
	}{{"desk.refund", 0}, {"desk.payout", 2 * time.Hour}} {
		kind := validKind(k.slug)
		kind.CoolingOff = k.cooling
		kind.Effect = func(ctx context.Context, _ pgx.Tx, a Approved) error {
			// The effect context carries the approving decision, which is not the gate id.
			if d, ok := DecisionOf(ctx); !ok || d != "decision-"+a.GateID {
				t.Errorf("DecisionOf = (%q, %v), want the decision of %s", d, ok, a.GateID)
			}
			calls[a.GateID]++
			return nil
		}
		if err := e.Register(kind); err != nil {
			t.Fatal(err)
		}
	}
	closed := now.Add(-time.Hour)
	approved := func(kind string) *engineGate {
		return &engineGate{kind: kind, status: StatusApproved, requester: "prep", closedAt: &closed,
			expiresAt: now.Add(time.Hour), jcs: []byte(`{"amount_cents":5}`)}
	}
	tx := &engineTx{gates: map[string]*engineGate{
		"due-1":   approved("desk.refund"),
		"due-2":   approved("desk.refund"),
		"cooling": approved("desk.payout"), // closed 1h ago, cooling-off 2h: not due yet
		"pending": {kind: "desk.refund", status: StatusPending, requester: "prep", expiresAt: now.Add(time.Hour)},
	}}
	ctx := engineCtx(t, now)
	tests := []struct {
		name string
		want int
	}{
		{"first run runs every due effect", 2},
		{"second run finds nothing left", 0},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := e.RunDue(ctx, tx)
			if err != nil {
				t.Fatalf("RunDue: %v", err)
			}
			if n != tt.want {
				t.Fatalf("RunDue = %d, want %d", n, tt.want)
			}
			if tx.expireRun != i+1 {
				t.Fatalf("ExpireDue ran %d times, want %d", tx.expireRun, i+1)
			}
		})
	}
	want := map[string]int{"due-1": 1, "due-2": 1}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("effect calls = %v, want %v", calls, want)
	}
}

func TestRunDueSkipsFailedEffect(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	e := NewPG()
	kind := validKind("desk.refund")
	kind.Effect = func(context.Context, pgx.Tx, Approved) error { return errors.New("precondition gone") }
	if err := e.Register(kind); err != nil {
		t.Fatal(err)
	}
	closed := now.Add(-time.Minute)
	tx := &engineTx{gates: map[string]*engineGate{
		"g1": {kind: "desk.refund", status: StatusApproved, requester: "prep", closedAt: &closed,
			expiresAt: now.Add(time.Hour), jcs: []byte(`{"amount_cents":5}`)},
	}}
	n, err := e.RunDue(engineCtx(t, now), tx)
	if err != nil || n != 0 {
		t.Fatalf("RunDue = (%d, %v), want (0, nil)", n, err)
	}
	if !tx.gates["g1"].effectRan {
		t.Fatal("failed effect was not recorded")
	}
}

func TestGet(t *testing.T) {
	closed := time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC)
	gates := map[string]*engineGate{
		"g1": {kind: "desk.refund", status: StatusApproved, requester: "prep", closedAt: &closed,
			expiresAt: closed.Add(time.Hour), jcs: []byte(`{"amount_cents":5}`), effectRan: true},
		"g2": {kind: "desk.refund", status: StatusPending, requester: "prep", runID: "run-1",
			expiresAt: closed.Add(time.Hour), jcs: []byte(`{"amount_cents":7}`)},
	}
	closedDB := dbfake.New()
	closedDB.Close()
	tests := []struct {
		name     string
		db       TxRunner
		id       GateID
		want     error
		preparer Principal
	}{
		{"user-prepared gate", engineRunner{&engineTx{gates: gates}}, "g1", nil, Principal{Kind: PrincipalUser, UserID: "prep"}},
		{"agent-prepared gate", engineRunner{&engineTx{gates: gates}}, "g2", nil, Principal{Kind: PrincipalAgent, RunID: "run-1", ActingFor: "prep"}},
		{"missing gate", engineRunner{&engineTx{gates: gates}}, "nope", ErrNotFound, Principal{}},
		{"no database", nil, "g1", ErrNoDB, Principal{}},
		{"closed database", closedDB, "g1", db.ErrClosed, Principal{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewPG()
			if tt.db != nil {
				e.SetDB(tt.db)
			}
			g, err := e.Get(context.Background(), tt.id)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("Get = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				return
			}
			src := gates[string(tt.id)]
			if g.ID != tt.id || g.Kind != src.kind || g.Status != src.status || string(g.Payload) != string(src.jcs) {
				t.Fatalf("Get = %+v", g)
			}
			if !reflect.DeepEqual(g.Preparer, tt.preparer) {
				t.Fatalf("Preparer = %+v, want %+v", g.Preparer, tt.preparer)
			}
			if g.EffectRan != src.effectRan || !strings.HasPrefix(g.PayloadHash, "sha256:") {
				t.Fatalf("EffectRan %v hash %q", g.EffectRan, g.PayloadHash)
			}
			if len(g.Approvers) != 1 || g.Approvers[0].UserID != "approver" {
				t.Fatalf("Approvers = %+v", g.Approvers)
			}
		})
	}
}
