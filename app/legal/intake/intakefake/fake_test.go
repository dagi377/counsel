package intakefake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/intake"
)

var t0 = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func newFake() *Fake {
	f := New(func() time.Time { return t0 })
	f.AddForm("f1", true)
	f.AddForm("draft", false)
	return f
}

func TestSubmit(t *testing.T) {
	tests := []struct {
		name string
		s    intake.Submission
		want error
	}{
		{name: "ok", s: intake.Submission{FormID: "f1", ContactName: "A", ContactEmail: "a@example.com"}},
		{name: "unpublished", s: intake.Submission{FormID: "draft", ContactName: "A", ContactEmail: "a@example.com"}, want: intake.ErrFormNotPublished},
		{name: "unknown form", s: intake.Submission{FormID: "zz", ContactName: "A", ContactEmail: "a@example.com"}, want: intake.ErrNotFound},
		{name: "no contact", s: intake.Submission{FormID: "f1", ContactName: "A"}, want: intake.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := newFake().Submit(context.Background(), tt.s)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Submit err = %v, want %v", err, tt.want)
			}
			if err == nil && (l.ContactID == "" || l.Status != intake.LeadOpen || !l.CreatedAt.Equal(t0)) {
				t.Fatalf("lead = %+v", l)
			}
		})
	}
}

func TestSubmitDuplicate(t *testing.T) {
	f := newFake()
	s := intake.Submission{FormID: "f1", ContactName: "A", ContactEmail: "a@example.com", IdempotencyKey: "k"}
	a, _ := f.Submit(context.Background(), s)
	b, _ := f.Submit(context.Background(), s)
	if a.ID != b.ID {
		t.Fatalf("two leads %s and %s, want one", a.ID, b.ID)
	}
}

func TestConvert(t *testing.T) {
	tests := []struct {
		name    string
		book    bool
		outcome intake.Outcome
		clear   bool
		want    error
	}{
		{name: "converts", book: true, outcome: intake.OutcomeRetain, clear: true},
		{name: "no consultation", clear: true, want: intake.ErrOutcomeRequired},
		{name: "no outcome", book: true, clear: true, want: intake.ErrOutcomeRequired},
		{name: "conflict open", book: true, outcome: intake.OutcomeRetain, want: intake.ErrConflictNotCleared},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFake()
			l, err := f.Submit(ctx, intake.Submission{FormID: "f1", ContactName: "A", ContactPhone: "555"})
			if err != nil {
				t.Fatal(err)
			}
			if tt.book {
				c, err := f.Book(ctx, l.ID, "lawyer", t0.Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if tt.outcome != intake.OutcomeNone {
					if _, err := f.RecordOutcome(ctx, c.ID, tt.outcome); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tt.clear {
				f.ClearConflict(l.ID)
			}
			matter, err := f.Convert(ctx, l.ID, "lawyer")
			if !errors.Is(err, tt.want) {
				t.Fatalf("Convert err = %v, want %v", err, tt.want)
			}
			if err == nil {
				got, _ := f.Lead(ctx, l.ID)
				if got.Status != intake.LeadConverted || got.MatterID != matter || matter == "" {
					t.Fatalf("lead = %+v, matter %q", got, matter)
				}
				if _, err := f.Decline(ctx, l.ID, "lawyer"); !errors.Is(err, intake.ErrLeadClosed) {
					t.Fatalf("Decline after convert err = %v", err)
				}
			}
		})
	}
}

func TestDecline(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	l, _ := f.Submit(ctx, intake.Submission{FormID: "f1", ContactName: "A", ContactPhone: "555"})
	got, err := f.Decline(ctx, l.ID, "lawyer")
	if err != nil || got.Status != intake.LeadSealedProspect {
		t.Fatalf("Decline = %+v, %v", got, err)
	}
	if _, err := f.Book(ctx, l.ID, "lawyer", t0); !errors.Is(err, intake.ErrLeadClosed) {
		t.Fatalf("Book on declined lead err = %v", err)
	}
	if _, err := f.Lead(ctx, "zz"); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("Lead err = %v", err)
	}
}
