package boot

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/limitation"
	"github.com/dagi377/counsel/app/legal/limitation/limitationfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
)

// The Assessor boot builds computes on the holiday-aware Statute, never a bare one.
func TestLimitationAssessorUsesHolidayStatute(t *testing.T) {
	tests := []struct {
		name      string
		store     limitation.Store
		deadlines deadline.Engine
		wantErr   error
	}{
		{"all ports", limitationfake.New(), deadlinefake.New(), nil},
		{"no store", nil, deadlinefake.New(), ErrJobDependency},
		{"no deadline engine", limitationfake.New(), nil, ErrJobDependency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := LimitationAssessor(nil, tt.store, tt.deadlines)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("LimitationAssessor() = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			// Canada Day 2027 is a holiday: the basic period rolls to Friday 2027-07-02.
			ctx := legalflagstest.Context(t, legalflags.DeadlineOntarioPack, true)
			ps, err := a.Calc.Compute(ctx, limitation.Assessment{MatterID: "m", ActDate: actDate(t, "2025-07-01"), DiscoveryPresumed: true})
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			var basic string
			for _, p := range ps {
				if p.Kind == "basic" {
					basic = p.Due.Format(time.DateOnly)
				}
			}
			if basic != "2027-07-02" {
				t.Fatalf("basic = %q, want 2027-07-02 (periods %v)", basic, ps)
			}
		})
	}
}
