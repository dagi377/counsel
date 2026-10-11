package workfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/work"
)

func valid() work.Item {
	return work.Item{Type: "task", Title: "Draft", WorkflowID: "wf", StatusID: "todo", ReporterID: "u1", AssigneeUserID: "u2"}
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		mutate  func(*work.Item)
		wantErr error
	}{
		{name: "ok", mutate: func(*work.Item) {}},
		{name: "no title", mutate: func(i *work.Item) { i.Title = "" }, wantErr: work.ErrInvalidItem},
		{name: "no reporter", mutate: func(i *work.Item) { i.ReporterID = "" }, wantErr: work.ErrInvalidItem},
		{name: "priority too high", mutate: func(i *work.Item) { i.Priority = 6 }, wantErr: work.ErrInvalidItem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			it := valid()
			tt.mutate(&it)
			id, err := f.Create(ctx, nil, it)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			got, err := f.Get(ctx, id)
			if err != nil || got.Priority != 3 || got.Version != 1 || got.Key == "" {
				t.Fatalf("Get = %+v, %v", got, err)
			}
		})
	}
}

func TestMoveAndApprover(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		op      func(f *Fake, id work.ItemID) error
		wantErr error
	}{
		{name: "allowed move", op: func(f *Fake, id work.ItemID) error { return f.Move(ctx, nil, id, 1, "doing", "u2") }},
		{name: "no transition", op: func(f *Fake, id work.ItemID) error { return f.Move(ctx, nil, id, 1, "done", "u2") }, wantErr: work.ErrTransitionNotAllowed},
		{name: "stale version", op: func(f *Fake, id work.ItemID) error { return f.Move(ctx, nil, id, 7, "doing", "u2") }, wantErr: work.ErrStale},
		{name: "unknown item", op: func(f *Fake, _ work.ItemID) error { return f.Move(ctx, nil, "x", 1, "doing", "u2") }, wantErr: work.ErrNotFound},
		{name: "approver ok", op: func(f *Fake, id work.ItemID) error { return f.SetApprover(ctx, nil, id, "u3", false) }},
		{name: "preparer cannot approve", op: func(f *Fake, id work.ItemID) error { return f.SetApprover(ctx, nil, id, "u2", false) }, wantErr: work.ErrSelfApproval},
		{name: "agent cannot approve", op: func(f *Fake, id work.ItemID) error { return f.SetApprover(ctx, nil, id, "run1", true) }, wantErr: work.ErrSelfApproval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			f.AddTransition(work.Transition{WorkflowID: "wf", From: "todo", To: "doing"})
			id, err := f.Create(ctx, nil, valid())
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.op(f, id); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	f := New()
	for _, s := range []work.Subject{{Kind: "case", ID: "1"}, {Kind: "case", ID: "1"}, {Kind: "case", ID: "2"}} {
		it := valid()
		it.Subject = s
		if _, err := f.Create(ctx, nil, it); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		flt  work.Filter
		want int
	}{
		{name: "all", want: 3},
		{name: "by subject", flt: work.Filter{Subject: work.Subject{Kind: "case", ID: "1"}}, want: 2},
		{name: "limit", flt: work.Filter{Limit: 1}, want: 1},
		{name: "by status none", flt: work.Filter{StatusID: "done"}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.List(ctx, tt.flt)
			if err != nil || len(got) != tt.want {
				t.Fatalf("List = %d, %v; want %d", len(got), err, tt.want)
			}
		})
	}
}

func TestAutomations(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		a       work.Automation
		wantErr error
		wantN   int
	}{
		{name: "no effect", a: work.Automation{Trigger: "item.created", Enabled: true, Action: work.ActionRef{Name: "notify"}}, wantN: 1},
		{name: "effect with gate", a: work.Automation{Trigger: "item.created", Enabled: true, Action: work.ActionRef{Name: "pay", HasEffect: true, GateKind: "payment"}}, wantN: 1},
		{name: "effect without gate", a: work.Automation{Trigger: "item.created", Enabled: true, Action: work.ActionRef{Name: "pay", HasEffect: true}}, wantErr: work.ErrEffectNeedsGate},
		{name: "disabled not returned", a: work.Automation{Trigger: "item.created", Action: work.ActionRef{Name: "notify"}}, wantN: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if _, err := f.Save(ctx, nil, tt.a); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			got, err := f.ForTrigger(ctx, "item.created")
			if err != nil || len(got) != tt.wantN {
				t.Fatalf("ForTrigger = %d, %v; want %d", len(got), err, tt.wantN)
			}
		})
	}
}
