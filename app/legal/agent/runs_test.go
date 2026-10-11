package agent_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/registry"
)

// memRecords is an in-memory agent.RunRecords.
type memRecords struct {
	mu   sync.Mutex
	rows map[string]run.Record
}

func (m *memRecords) Create(_ context.Context, r run.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.ID] = r
	return nil
}

func (m *memRecords) Start(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	r.State, r.StartedAt = run.StateRunning, &at
	m.rows[id] = r
	return nil
}

func (m *memRecords) Finish(_ context.Context, id string, at time.Time, f run.Finish) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	r.State, r.ErrorCode, r.EndedAt = f.State, f.ErrorCode, &at
	r.InTokens, r.OutTokens, r.CostMicroC = f.InTokens, f.OutTokens, f.CostMicroC
	m.rows[id] = r
	return nil
}

func (m *memRecords) Get(_ context.Context, id string) (run.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return run.Record{}, run.ErrRecordNotFound
	}
	return r, nil
}

const (
	runID   = "0190e000-0000-7000-8000-000000000001"
	runUser = "0190d000-0000-7000-8000-000000000001"
)

func runFeatures(t *testing.T) *feature.Set {
	t.Helper()
	s := feature.NewSet()
	if err := s.Add(feature.Def{ID: "F-TST-02", Area: "test", Name: "Synthetic run", Source: feature.SourceScreenAction,
		Grant:  []string{"agent.ask", "documents.view"},
		Prompt: fstest.MapFS{"p.md": {Data: []byte("Answer.\n")}}, PromptPath: "p.md"}); err != nil {
		t.Fatal(err)
	}
	return s
}

// runFixture ends the run on the first turn (mock model only).
var runFixture = fstest.MapFS{"f-tst-02/run/ok.yaml": {Data: []byte(`synthetic: true
feature: F-TST-02
match:
  last_user_regex: 'skill'
turns:
  - reply:
      text: "Done."
      stop_reason: end_turn
`)}}

// rule: ARC-I-036
// rule: AI-R-005
func TestRunStarter(t *testing.T) {
	ctx := context.Background()
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: runID, ActingFor: runUser, Grant: []string{"agent.ask"}}
	tests := []struct {
		name      string
		skill     string
		principal registry.Principal
		opts      []aifake.Option
		err       error
		state     string
		code      string
	}{
		{"runs to success as the agent", "f-tst-02", agentP, nil, nil, run.StateSucceeded, ""},
		{"busy model fails the run with a code", "f-tst-02", agentP,
			[]aifake.Option{aifake.WithFault("F-TST-02", aifake.FaultRateLimit)}, nil, run.StateFailed, "model_unavailable"},
		{"unknown skill", "f-none-01", agentP, nil, agent.ErrUnknownSkill, "", ""},
		{"user principal is refused", "f-tst-02",
			registry.Principal{Kind: registry.PrincipalUser, UserID: runUser}, nil, run.ErrNotAgent, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs := &memRecords{rows: map[string]run.Record{}}
			s := &agent.RunStarter{Features: runFeatures(t), Model: aifake.New(runFixture, tt.opts...),
				Tools: agenttoolsfake.New(), Records: recs, Go: func(f func()) { f() }}
			got, err := s.Start(ctx, agent.StartRun{ID: runID, SkillID: tt.skill, Principal: tt.principal,
				Inputs: []byte(`{"note":"client secret text"}`)})
			if !errors.Is(err, tt.err) {
				t.Fatalf("Start err = %v, want %v", err, tt.err)
			}
			if err != nil {
				if len(recs.rows) != 0 {
					t.Fatal("refused run was recorded")
				}
				return
			}
			if got.State != agent.RunQueued || got.ActingFor != runUser || !slices.Equal(got.Grant, []string{"agent.ask"}) {
				t.Fatalf("Start = %+v, want queued run acting for %s with the narrowed grant", got, runUser)
			}
			after, err := s.Run(ctx, runID)
			if err != nil || after.State != tt.state {
				t.Fatalf("Run = %+v, %v; want state %s", after, err, tt.state)
			}
			if rec := recs.rows[runID]; rec.ErrorCode != tt.code || rec.FeatureID != "F-TST-02" || rec.EndedAt == nil {
				t.Fatalf("record = %+v, want code %q", rec, tt.code)
			}
		})
	}
	s := &agent.RunStarter{Records: &memRecords{rows: map[string]run.Record{}}}
	if _, err := s.Run(ctx, runID); !errors.Is(err, agent.ErrRunNotFound) {
		t.Fatalf("Run unknown = %v, want ErrRunNotFound", err)
	}
	g, err := (&agent.RunStarter{Features: runFeatures(t)}).SkillGrant(ctx, "f-tst-02")
	if err != nil || !slices.Equal(g, []string{"agent.ask", "documents.view"}) {
		t.Fatalf("SkillGrant = %v, %v", g, err)
	}
}
