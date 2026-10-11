// Package worktest holds the behaviour every kernel/work Service must show. The in-memory
// workfake and the Postgres work.PG run the same tests, so the fake cannot drift from the
// real store. No network, Postgres or Docker of its own: the caller supplies the store.
package worktest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/work"
)

// Env is one fresh store with a workflow ready to use.
type Env struct {
	Svc work.Service
	// InTx runs fn in a transaction that commits when fn returns nil (the fake passes nil).
	InTx func(ctx context.Context, fn func(tx pgx.Tx) error) error
	// WorkflowID has statuses Todo, Doing and Done and one transition, Todo -> Doing.
	WorkflowID        string
	Todo, Doing, Done string
	// Users are three distinct user ids: reporter, assignee (preparer), approver.
	Users [3]string
	// SubjectKind is a registered subject kind; SubjectIDs are two distinct subject ids.
	SubjectKind string
	SubjectIDs  [2]string
	// MissingID is a well-formed item id that names no item.
	MissingID work.ItemID
}

// Run runs the contract tests. newEnv returns a fresh, empty store for each subtest.
func Run(t *testing.T, newEnv func(t *testing.T) Env) {
	t.Run("Create", func(t *testing.T) { testCreate(t, newEnv) })
	t.Run("Move", func(t *testing.T) { testMove(t, newEnv) })
	t.Run("SetApprover", func(t *testing.T) { testSetApprover(t, newEnv) })
	t.Run("List", func(t *testing.T) { testList(t, newEnv) })
}

func item(e Env) work.Item {
	desc := "Draft the **reply**."
	return work.Item{
		Type: "task", Title: "Draft", Description: &desc, WorkflowID: e.WorkflowID, StatusID: e.Todo,
		ReporterID: e.Users[0], AssigneeUserID: e.Users[1], Labels: []string{"a", "b"},
	}
}

func create(t *testing.T, e Env, it work.Item) work.ItemID {
	t.Helper()
	var id work.ItemID
	err := e.InTx(context.Background(), func(tx pgx.Tx) error {
		var err error
		id, err = e.Svc.Create(context.Background(), tx, it)
		return err
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return id
}

func testCreate(t *testing.T, newEnv func(t *testing.T) Env) {
	ctx := context.Background()
	tests := []struct {
		name    string
		mutate  func(*work.Item)
		wantErr error
	}{
		{name: "ok", mutate: func(*work.Item) {}},
		{name: "no title", mutate: func(i *work.Item) { i.Title = "" }, wantErr: work.ErrInvalidItem},
		{name: "no type", mutate: func(i *work.Item) { i.Type = "" }, wantErr: work.ErrInvalidItem},
		{name: "no reporter", mutate: func(i *work.Item) { i.ReporterID = "" }, wantErr: work.ErrInvalidItem},
		{name: "priority too high", mutate: func(i *work.Item) { i.Priority = 6 }, wantErr: work.ErrInvalidItem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			it := item(e)
			tt.mutate(&it)
			var id work.ItemID
			err := e.InTx(ctx, func(tx pgx.Tx) error {
				var err error
				id, err = e.Svc.Create(ctx, tx, it)
				return err
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			got, err := e.Svc.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.ID != id || got.Key == "" || got.Version != 1 || got.Priority != work.DefaultPriority {
				t.Fatalf("Get = %+v; want id %s, a key, version 1, priority %d", got, id, work.DefaultPriority)
			}
			if got.Title != it.Title || got.Type != it.Type || got.WorkflowID != it.WorkflowID ||
				got.StatusID != it.StatusID || got.ReporterID != it.ReporterID ||
				got.AssigneeUserID != it.AssigneeUserID || !reflect.DeepEqual(got.Labels, it.Labels) ||
				!reflect.DeepEqual(got.Description, it.Description) {
				t.Fatalf("Get = %+v; fields differ from %+v", got, it)
			}
			if _, err := e.Svc.Get(ctx, e.MissingID); !errors.Is(err, work.ErrNotFound) {
				t.Fatalf("Get missing err = %v, want ErrNotFound", err)
			}
		})
	}
}

func testMove(t *testing.T, newEnv func(t *testing.T) Env) {
	ctx := context.Background()
	tests := []struct {
		name    string
		missing bool
		version int
		to      func(Env) string
		wantErr error
	}{
		{name: "allowed move", version: 1, to: func(e Env) string { return e.Doing }},
		{name: "no transition", version: 1, to: func(e Env) string { return e.Done }, wantErr: work.ErrTransitionNotAllowed},
		{name: "stale version", version: 7, to: func(e Env) string { return e.Doing }, wantErr: work.ErrStale},
		{name: "unknown item", missing: true, version: 1, to: func(e Env) string { return e.Doing }, wantErr: work.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			id := create(t, e, item(e))
			target := id
			if tt.missing {
				target = e.MissingID
			}
			err := e.InTx(ctx, func(tx pgx.Tx) error {
				return e.Svc.Move(ctx, tx, target, tt.version, tt.to(e), e.Users[1])
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Move err = %v, want %v", err, tt.wantErr)
			}
			got, err := e.Svc.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			wantStatus, wantVersion := e.Todo, 1
			if tt.wantErr == nil {
				wantStatus, wantVersion = tt.to(e), 2
			}
			if got.StatusID != wantStatus || got.Version != wantVersion {
				t.Fatalf("after Move: status %s version %d, want %s %d", got.StatusID, got.Version, wantStatus, wantVersion)
			}
		})
	}
}

func testSetApprover(t *testing.T, newEnv func(t *testing.T) Env) {
	ctx := context.Background()
	tests := []struct {
		name     string
		approver func(Env) string
		isAgent  bool
		wantErr  error
	}{
		{name: "approver ok", approver: func(e Env) string { return e.Users[2] }},
		// rule: the person who prepares an item never approves it.
		{name: "preparer cannot approve", approver: func(e Env) string { return e.Users[1] }, wantErr: work.ErrSelfApproval},
		// rule: the agent never approves.
		{name: "agent cannot approve", approver: func(e Env) string { return e.Users[2] }, isAgent: true, wantErr: work.ErrSelfApproval},
		{name: "empty approver", approver: func(Env) string { return "" }, wantErr: work.ErrSelfApproval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			id := create(t, e, item(e))
			err := e.InTx(ctx, func(tx pgx.Tx) error {
				return e.Svc.SetApprover(ctx, tx, id, tt.approver(e), tt.isAgent)
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SetApprover err = %v, want %v", err, tt.wantErr)
			}
			got, err := e.Svc.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			wantApprover, wantVersion := "", 1
			if tt.wantErr == nil {
				wantApprover, wantVersion = tt.approver(e), 2
			}
			if got.ApproverID != wantApprover || got.Version != wantVersion {
				t.Fatalf("after SetApprover: approver %q version %d, want %q %d", got.ApproverID, got.Version, wantApprover, wantVersion)
			}
		})
	}
	t.Run("unknown item", func(t *testing.T) {
		e := newEnv(t)
		err := e.InTx(ctx, func(tx pgx.Tx) error {
			return e.Svc.SetApprover(ctx, tx, e.MissingID, e.Users[2], false)
		})
		if !errors.Is(err, work.ErrNotFound) {
			t.Fatalf("SetApprover err = %v, want ErrNotFound", err)
		}
	})
}

func testList(t *testing.T, newEnv func(t *testing.T) Env) {
	ctx := context.Background()
	e := newEnv(t)
	s1 := work.Subject{Kind: e.SubjectKind, ID: e.SubjectIDs[0]}
	s2 := work.Subject{Kind: e.SubjectKind, ID: e.SubjectIDs[1]}
	var ids []work.ItemID
	for _, s := range []work.Subject{s1, s1, s2} {
		it := item(e)
		it.Subject = s
		ids = append(ids, create(t, e, it))
	}
	tests := []struct {
		name string
		flt  work.Filter
		want []work.ItemID
	}{
		{name: "all", want: ids},
		{name: "by subject", flt: work.Filter{Subject: s1}, want: ids[:2]},
		{name: "by assignee", flt: work.Filter{AssigneeUserID: e.Users[1]}, want: ids},
		{name: "by assignee none", flt: work.Filter{AssigneeUserID: e.Users[2]}},
		{name: "limit", flt: work.Filter{Limit: 1}, want: ids[:1]},
		{name: "by status none", flt: work.Filter{StatusID: e.Done}},
		{name: "after first", flt: work.Filter{After: ids[0]}, want: ids[1:]},
		{name: "after first, limit", flt: work.Filter{After: ids[0], Limit: 1}, want: ids[1:2]},
		{name: "after last", flt: work.Filter{After: ids[2]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.Svc.List(ctx, tt.flt)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			var gotIDs []work.ItemID
			for _, it := range got {
				gotIDs = append(gotIDs, it.ID)
			}
			if !reflect.DeepEqual(gotIDs, tt.want) {
				t.Fatalf("List ids = %v, want %v", gotIDs, tt.want)
			}
		})
	}
	got, err := e.Svc.List(ctx, work.Filter{Subject: s2})
	if err != nil || len(got) != 1 || got[0].Subject != s2 {
		t.Fatalf("List by subject = %+v, %v; want one item on %v", got, err, s2)
	}
}
