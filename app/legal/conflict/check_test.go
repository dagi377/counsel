package conflict

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type nameIndex map[string][]Hit

func (n nameIndex) Search(_ context.Context, term string) ([]Hit, error) {
	if term == "boom" {
		return nil, errors.New("index down")
	}
	return n[strings.ToLower(term)], nil
}

func newChecks() *Checks {
	idx := nameIndex{"acme": {{Term: "acme", PartyID: "p1", MatterID: "m9", Role: "adverse"}}}
	return NewChecks(idx, func() time.Time { return time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC) })
}

var (
	preparer = Actor{UserID: "u-prep", IsLawyer: true}
	lawyer   = Actor{UserID: "u-law", IsLawyer: true}
	agent    = Actor{UserID: "agent-1", IsAgent: true, ActingFor: "u-law"}
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name     string
		req      Request
		wantErr  error
		wantHits int
		wantPrep string
	}{
		{"hit", Request{MatterID: "m1", Terms: []string{"Acme"}, Preparer: preparer}, nil, 1, "u-prep"},
		{"no hit", Request{MatterID: "m1", Terms: []string{"Other"}, Preparer: preparer}, nil, 0, "u-prep"},
		{"blank terms", Request{MatterID: "m1", Terms: []string{" ", ""}, Preparer: preparer}, ErrNoTerms, 0, ""},
		{"agent prepares for user", Request{MatterID: "m1", Terms: []string{"acme"}, Preparer: agent}, nil, 1, "u-law"},
		{"no preparer", Request{MatterID: "m1", Terms: []string{"acme"}}, errNoPreparer, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := newChecks().Check(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(r.Hits) != tt.wantHits || r.PreparedBy != tt.wantPrep || r.Status != StatusOpen {
				t.Fatalf("run = %+v", r)
			}
		})
	}
	t.Run("search error hides term", func(t *testing.T) {
		_, err := newChecks().Check(context.Background(), Request{Terms: []string{"boom"}, Preparer: preparer})
		if err == nil || strings.Contains(err.Error(), "boom") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunsAppendOnly(t *testing.T) {
	ctx := context.Background()
	c := newChecks()
	first, _ := c.Check(ctx, Request{MatterID: "m1", Terms: []string{"acme"}, Preparer: preparer})
	if _, err := c.Decide(ctx, first.ID, lawyer, false, "adverse party"); err != nil {
		t.Fatal(err)
	}
	second, _ := c.Check(ctx, Request{MatterID: "m1", Terms: []string{"acme"}, Preparer: preparer})
	_, _ = c.Check(ctx, Request{MatterID: "m2", Terms: []string{"acme"}, Preparer: preparer})

	runs, err := c.Runs(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		got    Run
		id     string
		status Status
	}{
		{"first run keeps its rejection", runs[0], first.ID, StatusRejected},
		{"re-check is a new open run", runs[1], second.ID, StatusOpen},
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(runs))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got.ID != tt.id || tt.got.Status != tt.status {
				t.Fatalf("run = %+v", tt.got)
			}
		})
	}
	t.Run("caller cannot edit stored run", func(t *testing.T) {
		runs[0].Hits[0].PartyID = "changed"
		runs[0].Terms[0] = "changed"
		again, _ := c.Runs(ctx, "m1")
		if again[0].Hits[0].PartyID != "p1" || again[0].Terms[0] != "acme" {
			t.Fatalf("stored run changed: %+v", again[0])
		}
	})
}

func TestDecideG2(t *testing.T) {
	tests := []struct {
		name        string
		approver    Actor
		approve     bool
		decideTwice bool
		wantErr     error
		wantStatus  Status
	}{
		{"other lawyer clears", lawyer, true, false, nil, StatusCleared},
		{"other lawyer rejects", lawyer, false, false, nil, StatusRejected},
		{"preparer cannot clear", preparer, true, false, ErrPreparerApproves, StatusOpen},
		{"agent cannot clear", agent, true, false, ErrAgentApproves, StatusOpen},
		{"agent's user is not preparer but agent still cannot", Actor{UserID: "a2", IsAgent: true, IsLawyer: true, ActingFor: "u-other"}, true, false, ErrAgentApproves, StatusOpen},
		{"non-lawyer cannot clear", Actor{UserID: "u-clerk"}, true, false, ErrNotLawyer, StatusOpen},
		{"no user", Actor{IsLawyer: true}, true, false, ErrPreparerApproves, StatusOpen},
		{"decided once only", lawyer, true, true, ErrDecided, StatusCleared},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c := newChecks()
			r, _ := c.Check(ctx, Request{MatterID: "m1", Terms: []string{"acme"}, Preparer: preparer})
			if err := c.Dispose(ctx, r.ID, 0, Actor{UserID: "u-law3", IsLawyer: true}, DispositionNotAConflict, "different Acme, other address"); err != nil {
				t.Fatal(err)
			}
			if tt.decideTwice {
				if _, err := c.Decide(ctx, r.ID, Actor{UserID: "u-law2", IsLawyer: true}, true, "ok"); err != nil {
					t.Fatal(err)
				}
			}
			got, err := c.Decide(ctx, r.ID, tt.approver, tt.approve, "reason")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", got.Status, tt.wantStatus)
			}
			if tt.wantErr == nil && (got.DecidedBy != tt.approver.UserID || got.DecidedAt.IsZero()) {
				t.Fatalf("decision not recorded: %+v", got)
			}
		})
	}
	t.Run("unknown run", func(t *testing.T) {
		if _, err := newChecks().Decide(context.Background(), "run-x", lawyer, true, ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestG2ClearNeedsEveryHitDisposed(t *testing.T) {
	law3 := Actor{UserID: "u-law3", IsLawyer: true}
	type disp struct {
		hit  int
		d    Disposition
		note string
	}
	tests := []struct {
		name       string
		terms      []string
		disps      []disp
		approve    bool
		reason     string
		wantErr    error
		wantStatus Status
	}{
		{"no hits clears without dispositions", []string{"other"}, nil, true, "", nil, StatusCleared},
		{"undisposed hit blocks clearance", []string{"acme", "beta"}, []disp{{0, DispositionNotAConflict, "different party"}}, true, "ok", ErrUndisposedHits, StatusOpen},
		{"no dispositions blocks clearance", []string{"acme"}, nil, true, "ok", ErrUndisposedHits, StatusOpen},
		{"cleared with reason", []string{"acme"}, []disp{{0, DispositionNotAConflict, "different party"}}, true, "ok", nil, StatusCleared},
		{"waived by consent", []string{"acme"}, []disp{{0, DispositionNeedsConsent, "written consent doc-77"}}, true, "ok", nil, StatusCleared},
		{"walled off with consent", []string{"acme"}, []disp{{0, DispositionNeedsWall, "wall w-3 and consent doc-78"}}, true, "ok", nil, StatusCleared},
		{"every hit disposed", []string{"acme", "beta"}, []disp{{0, DispositionNeedsConsent, "consent doc-77"}, {1, DispositionNeedsWall, "wall w-3"}}, true, "ok", nil, StatusCleared},
		{"real conflict cannot clear", []string{"acme"}, []disp{{0, DispositionConflict, "acts against client"}}, true, "ok", ErrUndisposedHits, StatusOpen},
		{"latest disposition counts", []string{"acme"}, []disp{{0, DispositionNotAConflict, "first look"}, {0, DispositionConflict, "second look"}}, true, "ok", ErrUndisposedHits, StatusOpen},
		{"clearing hits needs a reason", []string{"acme"}, []disp{{0, DispositionNotAConflict, "different party"}}, true, "  ", ErrDispositionNote, StatusOpen},
		{"reject needs no dispositions", []string{"acme"}, nil, false, "", nil, StatusRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			idx := nameIndex{
				"acme": {{Term: "acme", PartyID: "p1", MatterID: "m9", Role: "adverse"}},
				"beta": {{Term: "beta", PartyID: "p2", MatterID: "m8", Role: "adverse"}},
			}
			c := NewChecks(idx, func() time.Time { return time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC) })
			r, err := c.Check(ctx, Request{MatterID: "m1", Terms: tt.terms, Preparer: preparer})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range tt.disps {
				if err := c.Dispose(ctx, r.ID, d.hit, law3, d.d, d.note); err != nil {
					t.Fatalf("Dispose: %v", err)
				}
			}
			got, err := c.Decide(ctx, r.ID, lawyer, tt.approve, tt.reason)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", got.Status, tt.wantStatus)
			}
		})
	}
}

func TestDisposeChecks(t *testing.T) {
	tests := []struct {
		name    string
		hit     int
		by      Actor
		d       Disposition
		note    string
		decided bool
		wantErr error
	}{
		{"lawyer disposes", 0, lawyer, DispositionNotAConflict, "different party", false, nil},
		{"note required", 0, lawyer, DispositionNeedsConsent, " ", false, ErrDispositionNote},
		{"unknown disposition", 0, lawyer, Disposition("waived-ish"), "x", false, ErrBadDisposition},
		{"hit out of range", 1, lawyer, DispositionNotAConflict, "x", false, ErrBadDisposition},
		{"negative hit", -1, lawyer, DispositionNotAConflict, "x", false, ErrBadDisposition},
		{"agent cannot dispose", 0, agent, DispositionNotAConflict, "x", false, ErrAgentApproves},
		{"preparer cannot dispose", 0, preparer, DispositionNotAConflict, "x", false, ErrPreparerApproves},
		{"non-lawyer cannot dispose", 0, Actor{UserID: "u-clerk"}, DispositionNotAConflict, "x", false, ErrNotLawyer},
		{"decided run is closed", 0, lawyer, DispositionNotAConflict, "x", true, ErrDecided},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c := newChecks()
			r, _ := c.Check(ctx, Request{MatterID: "m1", Terms: []string{"acme"}, Preparer: preparer})
			if tt.decided {
				if _, err := c.Decide(ctx, r.ID, lawyer, false, "adverse party"); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Dispose(ctx, r.ID, tt.hit, tt.by, tt.d, tt.note); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
	t.Run("unknown run", func(t *testing.T) {
		if err := newChecks().Dispose(context.Background(), "run-x", 0, lawyer, DispositionNotAConflict, "x"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}
