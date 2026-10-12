package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/registry"
)

// firstRecords is an in-memory run record store.
type firstRecords struct {
	recs      map[string]agentrun.Record
	getErr    error
	createErr error
	creates   int
}

func (m *firstRecords) Get(_ context.Context, id string) (agentrun.Record, error) {
	if m.getErr != nil {
		return agentrun.Record{}, m.getErr
	}
	r, ok := m.recs[id]
	if !ok {
		return agentrun.Record{}, agentrun.ErrRecordNotFound
	}
	return r, nil
}

func (m *firstRecords) Create(_ context.Context, r agentrun.Record) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.creates++
	m.recs[r.ID] = r
	return nil
}

// seenRunner reports whether the run was recorded when it started, the way a batch draft's
// preparer lookup reads agent.run_record.acting_for.
type seenRunner struct {
	records *firstRecords
	ran     bool
	actor   string
}

func (s *seenRunner) Run(ctx context.Context, _ agentrun.Spec, p agentrun.Principal, _ string, _ ...agentrun.Trigger) (agentrun.Outcome, error) {
	s.ran = true
	if r, err := s.records.Get(ctx, p.RunID); err == nil {
		s.actor = r.ActingFor
	}
	return agentrun.Outcome{}, nil
}

// TestRecordFirst: every runner boot hands out records its run before the run starts, so a
// batch draft built in the run finds its preparer. A run already recorded is not recorded
// again; with no store, or a store that fails, nothing runs (fail closed).
//
// rule: AI-R-005
func TestRecordFirst(t *testing.T) {
	const run = "11111111-1111-4111-8111-111111111111"
	p := agentrun.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: "u-lawyer", Grant: []string{"billing.draft"}}
	spec := agentrun.Spec{FeatureID: "F-BIL-01"}
	boom := errors.New("db down")
	tests := []struct {
		name    string
		records func() *firstRecords
		nilRecs bool
		p       agentrun.Principal
		want    error
		creates int
	}{
		{"unrecorded run is recorded first", func() *firstRecords { return &firstRecords{recs: map[string]agentrun.Record{}} }, false, p, nil, 1},
		{"recorded run is not recorded again", func() *firstRecords {
			return &firstRecords{recs: map[string]agentrun.Record{run: {ID: run, ActingFor: "u-lawyer"}}}
		}, false, p, nil, 0},
		{"no store", func() *firstRecords { return &firstRecords{recs: map[string]agentrun.Record{}} }, true, p, agent.ErrMissingDep, 0},
		{"store read fails", func() *firstRecords { return &firstRecords{recs: map[string]agentrun.Record{}, getErr: boom} }, false, p, boom, 0},
		{"store write fails", func() *firstRecords { return &firstRecords{recs: map[string]agentrun.Record{}, createErr: boom} }, false, p, boom, 0},
		{"run without an acting user", func() *firstRecords { return &firstRecords{recs: map[string]agentrun.Record{}} }, false,
			agentrun.Principal{Kind: registry.PrincipalAgent, RunID: run}, agentrun.ErrNotAgent, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recs := tc.records()
			inner := &seenRunner{records: recs}
			r := RecordFirstRunner{Runner: inner, Records: recs}
			if tc.nilRecs {
				r.Records = nil
			}
			_, err := r.Run(context.Background(), spec, tc.p, "prompt")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if recs.creates != tc.creates {
				t.Fatalf("creates = %d, want %d", recs.creates, tc.creates)
			}
			if err != nil {
				if inner.ran {
					t.Fatal("refused run reached the runner")
				}
				return
			}
			if inner.actor != "u-lawyer" {
				t.Fatalf("run started unrecorded (acting_for %q)", inner.actor)
			}
			if got := recs.recs[run]; tc.creates == 1 && (got.FeatureID != "F-BIL-01" || got.SkillID != "f-bil-01" || got.State != agentrun.StateQueued) {
				t.Fatalf("record = %+v", got)
			}
		})
	}
}
