package work

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	uWF     = "0190a000-0000-7000-8000-000000000001"
	uTodo   = "0190a000-0000-7000-8000-000000000002"
	uDoing  = "0190a000-0000-7000-8000-000000000003"
	uRep    = "0190a000-0000-7000-8000-000000000004"
	uAsg    = "0190a000-0000-7000-8000-000000000005"
	uApr    = "0190a000-0000-7000-8000-000000000006"
	uItem   = "0190a000-0000-7000-8000-000000000007"
	uParent = "0190a000-0000-7000-8000-000000000008"
)

// row is one scripted QueryRow answer: values copied into the Scan destinations, or err.
type row struct {
	vals []any
	err  error
}

func (r row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i < len(r.vals) && r.vals[i] != nil {
			reflect.ValueOf(d).Elem().Set(reflect.ValueOf(r.vals[i]))
		}
	}
	return nil
}

// scriptTx answers QueryRow by SQL text from rows and records Exec calls. Other pgx.Tx
// methods panic.
type scriptTx struct {
	pgx.Tx
	rows    map[string]row
	queries []string
	execs   []string
	args    map[string][]any
}

func newTx(rows map[string]row) *scriptTx {
	return &scriptTx{rows: rows, args: map[string][]any{}}
}

func (s *scriptTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	s.queries = append(s.queries, sql)
	s.args[sql] = args
	r, ok := s.rows[sql]
	if !ok {
		return row{err: pgx.ErrNoRows}
	}
	return r
}

func (s *scriptTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.execs = append(s.execs, sql)
	s.args[sql] = args
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// txDB hands its one scripted tx to InTx.
type txDB struct {
	db.DB
	tx *scriptTx
}

func (d txDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

func kinds() *SubjectKinds {
	return NewSubjectKinds([]registry.Call{{Method: registry.MethodSubjectKind, Args: []any{registry.SubjectKind{ID: "desk.ticket"}}}})
}

func validItem() Item {
	return Item{Type: "task", Title: "Draft", WorkflowID: uWF, StatusID: uTodo, ReporterID: uRep, AssigneeUserID: uAsg}
}

func TestPGCreate(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		mutate    func(*Item)
		wantErr   error
		wantQuery bool
	}{
		{name: "ok", mutate: func(*Item) {}, wantQuery: true},
		{name: "no title", mutate: func(i *Item) { i.Title = " " }, wantErr: ErrInvalidItem},
		{name: "workflow not a uuid", mutate: func(i *Item) { i.WorkflowID = "wf" }, wantErr: ErrInvalidItem},
		{name: "parent not a uuid", mutate: func(i *Item) { i.ParentID = "p" }, wantErr: ErrInvalidItem},
		{name: "agent without user", mutate: func(i *Item) { i.AssigneeUserID, i.AssigneeIsAgent = "", true }, wantErr: ErrInvalidItem},
		{name: "unregistered subject", mutate: func(i *Item) { i.Subject = Subject{Kind: "other", ID: uParent} }, wantErr: ErrInvalidItem},
		{name: "blank description stored as none", mutate: func(i *Item) { b := "  "; i.Description = &b }, wantQuery: true},
		{name: "registered subject", mutate: func(i *Item) { i.Subject = Subject{Kind: "desk.ticket", ID: uParent} }, wantQuery: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTx(map[string]row{sqlCreate: {vals: []any{uItem}}})
			it := validItem()
			tt.mutate(&it)
			id, err := (&PG{Kinds: kinds()}).Create(ctx, tx, it)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create err = %v, want %v", err, tt.wantErr)
			}
			if got := len(tx.queries) == 1; got != tt.wantQuery {
				t.Fatalf("queries = %v, want query %v", tx.queries, tt.wantQuery)
			}
			if err != nil {
				return
			}
			args := tx.args[sqlCreate]
			if d := args[15].(*string); (it.Description == nil || strings.TrimSpace(*it.Description) == "") != (d == nil) {
				t.Fatalf("description arg = %v for %v", d, it.Description)
			}
			if id != uItem || args[11] != DefaultPriority || !reflect.DeepEqual(args[13], []string{}) {
				t.Fatalf("Create = %s with args %v; want id %s, priority %d, empty labels", id, args, uItem, DefaultPriority)
			}
		})
	}
}

func TestPGGet(t *testing.T) {
	ctx := context.Background()
	asg := uAsg
	desc := "Body"
	full := row{vals: []any{uItem, "W-1", "task", "Draft", &desc, uWF, uTodo, nil, nil, nil, &asg, false, nil, uRep, int16(3), nil, []string{"x"}, nil, 4}}
	tests := []struct {
		name    string
		id      ItemID
		rows    map[string]row
		wantErr error
		want    Item
	}{
		{name: "found", id: uItem, rows: map[string]row{sqlGet: full},
			want: Item{ID: uItem, Key: "W-1", Type: "task", Title: "Draft", Description: &desc, WorkflowID: uWF, StatusID: uTodo, AssigneeUserID: uAsg, ReporterID: uRep, Priority: 3, Labels: []string{"x"}, Version: 4}},
		{name: "missing", id: uItem, wantErr: ErrNotFound},
		{name: "malformed id", id: "nope", wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTx(tt.rows)
			got, err := (&PG{DB: txDB{tx: tx}}).Get(ctx, tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Get = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPGMove(t *testing.T) {
	ctx := context.Background()
	asg := uAsg
	locked := row{vals: []any{uWF, uTodo, &asg, 2}}
	tests := []struct {
		name     string
		id       ItemID
		version  int
		to       string
		rows     map[string]row
		wantErr  error
		wantExec bool
	}{
		{name: "allowed", id: uItem, version: 2, to: uDoing, rows: map[string]row{sqlLockItem: locked, sqlHasTransition: {vals: []any{true}}}, wantExec: true},
		{name: "no transition", id: uItem, version: 2, to: uDoing, rows: map[string]row{sqlLockItem: locked, sqlHasTransition: {vals: []any{false}}}, wantErr: ErrTransitionNotAllowed},
		{name: "target not a uuid", id: uItem, version: 2, to: "done", rows: map[string]row{sqlLockItem: locked}, wantErr: ErrTransitionNotAllowed},
		{name: "stale", id: uItem, version: 1, to: uDoing, rows: map[string]row{sqlLockItem: locked}, wantErr: ErrStale},
		{name: "missing", id: uItem, version: 2, to: uDoing, wantErr: ErrNotFound},
		{name: "malformed id", id: "x", version: 2, to: uDoing, wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTx(tt.rows)
			err := (&PG{}).Move(ctx, tx, tt.id, tt.version, tt.to, uAsg)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Move err = %v, want %v", err, tt.wantErr)
			}
			if got := len(tx.execs) == 1; got != tt.wantExec {
				t.Fatalf("execs = %v, want exec %v", tx.execs, tt.wantExec)
			}
		})
	}
}

func TestPGSetApprover(t *testing.T) {
	ctx := context.Background()
	asg := uAsg
	locked := map[string]row{sqlLockItem: {vals: []any{uWF, uTodo, &asg, 1}}}
	tests := []struct {
		name      string
		approver  string
		isAgent   bool
		rows      map[string]row
		wantErr   error
		wantQuery bool
		wantExec  bool
	}{
		{name: "ok", approver: uApr, rows: locked, wantQuery: true, wantExec: true},
		// rule: the person who prepares an item never approves it.
		{name: "preparer", approver: uAsg, rows: locked, wantErr: ErrSelfApproval, wantQuery: true},
		// rule: the agent never approves.
		{name: "agent", approver: uApr, isAgent: true, rows: locked, wantErr: ErrSelfApproval},
		{name: "empty", approver: "", rows: locked, wantErr: ErrSelfApproval},
		{name: "not a uuid", approver: "bob", rows: locked, wantErr: ErrInvalidItem},
		{name: "missing item", approver: uApr, wantErr: ErrNotFound, wantQuery: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTx(tt.rows)
			err := (&PG{}).SetApprover(ctx, tx, uItem, tt.approver, tt.isAgent)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetApprover err = %v, want %v", err, tt.wantErr)
			}
			if (len(tx.queries) > 0) != tt.wantQuery || (len(tx.execs) == 1) != tt.wantExec {
				t.Fatalf("queries %v execs %v; want query %v exec %v", tx.queries, tx.execs, tt.wantQuery, tt.wantExec)
			}
		})
	}
}

func TestPGListMalformedFilter(t *testing.T) {
	tests := []struct {
		name string
		f    Filter
	}{
		{name: "after", f: Filter{After: "item-1"}},
		{name: "status", f: Filter{StatusID: "todo"}},
		{name: "assignee", f: Filter{AssigneeUserID: "bob"}},
		{name: "subject", f: Filter{Subject: Subject{Kind: "desk.ticket", ID: "1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTx(nil)
			got, err := (&PG{DB: txDB{tx: tx}}).List(context.Background(), tt.f)
			if err != nil || got != nil || len(tx.queries) != 0 {
				t.Fatalf("List = %v, %v after %v; want no items and no query", got, err, tx.queries)
			}
		})
	}
}
