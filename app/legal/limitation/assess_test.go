package limitation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/limitation"
	"github.com/dagi377/counsel/app/legal/limitation/limitationfake"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/registry"
)

var (
	toronto, _ = time.LoadLocation("America/Toronto")
	clerk      = registry.Principal{Kind: registry.PrincipalUser, UserID: "u-clerk"}
	lawyer     = registry.Principal{Kind: registry.PrincipalUser, UserID: "u-lawyer"}
	agentRun   = registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u-clerk"}
)

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, toronto)
	return &t
}

func setup() (*limitation.Assessor, *deadlinefake.Fake) {
	f := limitationfake.New()
	dl := deadlinefake.New()
	return &limitation.Assessor{Calc: f, Store: f, Deadlines: dl}, dl
}

func matterAssessment() limitation.Assessment {
	return limitation.Assessment{MatterID: "m-1", ClaimType: "negligence",
		ActDate: day(2026, time.March, 2), DiscoveryDate: day(2026, time.April, 10)}
}

func TestAssessLinksMatterToProposedDeadlines(t *testing.T) {
	tests := []struct {
		name     string
		by       registry.Principal
		wantPrep string
	}{
		{"user prepares", clerk, "u-clerk"},
		{"agent prepares for its user", agentRun, "u-clerk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
			s, dl := setup()
			a, err := s.Assess(ctx, nil, matterAssessment(), tc.by)
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if a.PreparedBy != tc.wantPrep || a.ConfirmedBy != "" {
				t.Fatalf("prepared %q confirmed %q", a.PreparedBy, a.ConfirmedBy)
			}
			want := map[string]deadline.Date{
				a.BasicDeadlineID:    {Year: 2028, Month: time.April, Day: 10},
				a.UltimateDeadlineID: {Year: 2041, Month: time.March, Day: 2},
			}
			for id, due := range want {
				if id == "" {
					t.Fatalf("assessment not linked to deadlines: %+v", a)
				}
				d, err := dl.Get(ctx, deadline.DeadlineID(id))
				if err != nil {
					t.Fatalf("Get %s: %v", id, err)
				}
				if d.State != deadline.StateProposed {
					t.Fatalf("deadline %s state %s, want proposed until a person confirms", id, d.State)
				}
				if d.Proposed.Due != due || d.Proposed.Subject != (deadline.Subject{Kind: matterkind.Subject, ID: "m-1"}) {
					t.Fatalf("deadline %s = %+v, want due %s on matter m-1", id, d.Proposed, due)
				}
			}
		})
	}
}

func TestAssessRefused(t *testing.T) {
	lead := matterAssessment()
	lead.MatterID, lead.LeadID = "", "l-1"
	noDates := matterAssessment()
	noDates.DiscoveryDate = nil
	tests := []struct {
		name string
		on   bool
		a    limitation.Assessment
		by   registry.Principal
		want error
	}{
		{"flag off", false, matterAssessment(), clerk, limitation.ErrOff},
		{"lead has no deadline subject", true, lead, clerk, limitation.ErrOwner},
		{"missing dates", true, noDates, clerk, limitation.ErrDates},
		{"guest cannot prepare", true, matterAssessment(), registry.Principal{Kind: registry.PrincipalGuest, UserID: "g-1"}, limitation.ErrSelfConfirm},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, tc.on)
			s, _ := setup()
			if _, err := s.Assess(ctx, nil, tc.a, tc.by); !errors.Is(err, tc.want) {
				t.Fatalf("Assess err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAssessFlagUndefinedFailsClosed(t *testing.T) {
	s, _ := setup()
	if _, err := s.Assess(context.Background(), nil, matterAssessment(), clerk); !errors.Is(err, limitation.ErrOff) {
		t.Fatalf("Assess err = %v, want ErrOff", err)
	}
}

func TestConfirmNeedsAnotherPerson(t *testing.T) {
	tests := []struct {
		name      string
		by        registry.Principal
		twice     bool
		want      error
		confirmed bool
	}{
		{"other lawyer confirms", lawyer, false, nil, true},
		{"preparer refused", clerk, false, limitation.ErrSelfConfirm, false},
		{"agent refused", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-2", ActingFor: "u-lawyer"}, false, limitation.ErrSelfConfirm, false},
		{"guest refused", registry.Principal{Kind: registry.PrincipalGuest, UserID: "g-1"}, false, limitation.ErrSelfConfirm, false},
		{"second confirm refused", lawyer, true, limitation.ErrConfirmed, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
			s, dl := setup()
			a, err := s.Assess(ctx, nil, matterAssessment(), clerk)
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if tc.twice {
				if _, err := s.Confirm(ctx, nil, a.ID, tc.by); err != nil {
					t.Fatalf("first Confirm: %v", err)
				}
			}
			if _, err := s.Confirm(ctx, nil, a.ID, tc.by); !errors.Is(err, tc.want) {
				t.Fatalf("Confirm err = %v, want %v", err, tc.want)
			}
			got, err := s.Store.Get(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (got.ConfirmedBy == "u-lawyer") != tc.confirmed {
				t.Fatalf("assessment confirmed by %q, want confirmed %t", got.ConfirmedBy, tc.confirmed)
			}
			wantState := deadline.StateProposed
			if tc.confirmed {
				wantState = deadline.StateConfirmed
			}
			for _, id := range []string{a.BasicDeadlineID, a.UltimateDeadlineID} {
				d, err := dl.Get(ctx, deadline.DeadlineID(id))
				if err != nil {
					t.Fatal(err)
				}
				if d.State != wantState {
					t.Fatalf("deadline %s state %s, want %s", id, d.State, wantState)
				}
			}
		})
	}
}

func TestConfirmFlagOffAndUnknown(t *testing.T) {
	tests := []struct {
		name string
		on   bool
		id   string
		want error
	}{
		{"flag off", false, "la-1", limitation.ErrOff},
		{"unknown assessment", true, "la-404", limitation.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, tc.on)
			s, _ := setup()
			if _, err := s.Confirm(ctx, nil, tc.id, lawyer); !errors.Is(err, tc.want) {
				t.Fatalf("Confirm err = %v, want %v", err, tc.want)
			}
		})
	}
}
