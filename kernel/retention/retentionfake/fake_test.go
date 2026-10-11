package retentionfake

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/retention"
)

var today = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

func setup(t *testing.T) *Fake {
	t.Helper()
	f := New(func() time.Time { return today })
	for _, c := range []retention.Class{
		{ID: "ticket.standard", MinYears: 2, DestructionGate: "ticket.purge"},
		{ID: "ticket.forever", NeverPurge: true},
	} {
		if err := f.RegisterClass(c); err != nil {
			t.Fatal(err)
		}
	}
	f.AddRecord(retention.Record{ID: "old", Class: "ticket.standard", ClosedOn: today.AddDate(-3, 0, 0)})
	f.AddRecord(retention.Record{ID: "exact", Class: "ticket.standard", ClosedOn: today.AddDate(-2, 0, 0)})
	f.AddRecord(retention.Record{ID: "young", Class: "ticket.standard", ClosedOn: today.AddDate(-1, 0, 0)})
	f.AddRecord(retention.Record{ID: "held", Class: "ticket.standard", ClosedOn: today.AddDate(-5, 0, 0), Hold: true})
	f.AddRecord(retention.Record{ID: "open", Class: "ticket.standard"})
	f.AddRecord(retention.Record{ID: "kept", Class: "ticket.forever", ClosedOn: today.AddDate(-50, 0, 0)})
	return f
}

func TestRegisterClass(t *testing.T) {
	tests := []struct {
		name    string
		c       retention.Class
		wantErr error
	}{
		{"purgeable without gate", retention.Class{ID: "a.b", MinYears: 1}, retention.ErrInvalidClass},
		{"negative years", retention.Class{ID: "a.b", MinYears: -1, NeverPurge: true}, retention.ErrInvalidClass},
		{"duplicate", retention.Class{ID: "ticket.forever", NeverPurge: true}, retention.ErrDuplicateClass},
		{"ok", retention.Class{ID: "a.b", MinYears: 7, DestructionGate: "a.purge"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := setup(t).RegisterClass(tt.c); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestProposeDestroysNothing(t *testing.T) {
	tests := []struct {
		name    string
		class   string
		want    []string
		wantErr error
	}{
		{"past min years, no hold", "ticket.standard", []string{"exact", "old"}, nil},
		{"never purge is never proposed", "ticket.forever", nil, retention.ErrNeverPurge},
		{"unknown class", "ticket.nope", nil, retention.ErrUnknownClass},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			l, err := f.Propose(context.Background(), tt.class)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(l.Records, tt.want) {
				t.Fatalf("Records = %v, want %v", l.Records, tt.want)
			}
			for _, id := range []string{"old", "exact", "young", "held", "open", "kept"} {
				if !f.Exists(id) {
					t.Fatalf("%s destroyed by Propose", id)
				}
			}
		})
	}
}

func TestApplyNeedsDestructionGate(t *testing.T) {
	tests := []struct {
		name      string
		approveBy string // "" means no approval
		applyWith string
		wantErr   error
	}{
		{"not approved", "", "ticket.purge", retention.ErrNotApproved},
		{"approved by another gate", "other.gate", "other.gate", retention.ErrNotApproved},
		{"apply names a different gate", "ticket.purge", "other.gate", retention.ErrNotApproved},
		{"approved by destruction gate", "ticket.purge", "ticket.purge", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := setup(t)
			l, err := f.Propose(ctx, "ticket.standard")
			if err != nil {
				t.Fatal(err)
			}
			if tt.approveBy != "" {
				f.Approve(l.ID, tt.approveBy)
			}
			err = f.Apply(ctx, nil, l.ID, tt.applyWith)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			destroyed := err == nil
			if f.Exists("old") == destroyed || f.Exists("exact") == destroyed {
				t.Fatalf("destroyed = %v but old=%v exact=%v", destroyed, f.Exists("old"), f.Exists("exact"))
			}
			if !f.Exists("young") || !f.Exists("held") || !f.Exists("kept") {
				t.Fatal("record outside the list destroyed")
			}
		})
	}
}

func TestApplyUnknownList(t *testing.T) {
	if err := setup(t).Apply(context.Background(), nil, "nope", "ticket.purge"); !errors.Is(err, retention.ErrUnknownList) {
		t.Fatalf("err = %v", err)
	}
}
