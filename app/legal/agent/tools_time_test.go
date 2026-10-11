package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	timeMatter  = "81111111-1111-4111-8111-111111111111"
	timeWalled  = "82222222-2222-4222-8222-222222222222"
	timeRun     = "run-time"
	timeNoPlan  = "run-time-noplan"
	timeCapName = "time.record"
)

type timeWorld struct {
	reg     *agenttools.Registry
	entries *timekeepingfake.Fake
	gates   *gatefake.Fake
}

// newTimeWorld registers the real propose_time_entries on a Registry over module fakes and a
// fake gate engine. timeRun has a plan; timeNoPlan has none. clerk holds actorCaps and is
// walled from timeWalled.
func newTimeWorld(t *testing.T, actorCaps map[string]bool) timeWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, timeWalled)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := timeWorld{
		entries: timekeepingfake.New(nil, nil),
		gates:   gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, gatefake.Caps{clerk: {timeCapName}}),
	}
	err := w.gates.Register(gate.GateKind{
		Slug: agent.GateTimeAccept, Approver: timeCapName, Risk: gate.RiskMedium, MinApprovers: 1, TTL: 336 * time.Hour,
		Effect: func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		t.Fatalf("gate kind: %v", err)
	}
	open := agenttools.GateOpener(func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, by agenttools.Principal) (gate.GateID, error) {
		return w.gates.Open(ctx, nil, kind, s, payload, by)
	})
	var entries tk.Entries = w.entries
	// timeRun read a transcript quoting the dates and minutes the cases propose.
	pv := agenttools.NewProvenance()
	pv.Observe(timeRun, json.RawMessage(`{"untrusted":true,"text":"Calls on 2026-10-09 (30 minutes) and 2026-10-10 (6 minutes)."}`))
	pv.Observe(timeNoPlan, json.RawMessage(`{"work_date":"2026-10-09","minutes":30}`))
	tool, err := agent.ProposeTimeEntriesHandler().Build(agent.NewDeps(can, walls, entries, open, pv))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, timeRun, []string{"suggest time"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func timeInput(entries ...string) string {
	out := `{"entries":[`
	for i, e := range entries {
		if i > 0 {
			out += ","
		}
		out += e
	}
	return out + `]}`
}

func timeEntry(matterID, date string, minutes int) string {
	b, _ := json.Marshal(map[string]any{"matter_id": matterID, "work_date": date, "minutes": minutes, "narrative": "Synthetic review"})
	return string(b)
}

// timeScope is the run scope the Runner stores for a run whose feature declares kinds.
func timeScope(run string, kinds ...string) context.Context {
	return agenttools.WithScope(context.Background(), agenttools.NewScope(run, kinds))
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_ProposeTimeEntries(t *testing.T) {
	record := map[string]bool{timeCapName: true}
	grant := []string{timeCapName}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	one := timeInput(timeEntry(timeMatter, "2026-10-09", 30))
	cases := []struct {
		name      string
		ctx       context.Context
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantN     int
	}{
		{"agent suggests one entry", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record, one, nil, 1},
		{"agent suggests two entries", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record,
			timeInput(timeEntry(timeMatter, "2026-10-09", 30), timeEntry(timeMatter, "2026-10-10", 6)), nil, 2},
		{"manual path", context.Background(), user, record, one, nil, 1},
		{"walled matter", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record, timeInput(timeEntry(timeWalled, "2026-10-09", 30)), agent.ErrNotVisible, 0},
		{"one walled entry refuses the batch", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record,
			timeInput(timeEntry(timeMatter, "2026-10-09", 30), timeEntry(timeWalled, "2026-10-09", 30)), agent.ErrNotVisible, 0},
		{"walled matter on the manual path", context.Background(), user, record, timeInput(timeEntry(timeWalled, "2026-10-09", 30)), agent.ErrNotVisible, 0},
		{"run without the capability", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, nil), record, one, agenttools.ErrMissingCapability, 0},
		{"acting user without the capability", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), map[string]bool{}, one, agent.ErrForbidden, 0},
		{"no report_plan in the run", timeScope(timeNoPlan, agent.GateTimeAccept), agentP(timeNoPlan, grant), record, one, agenttools.ErrPlanRequired, 0},
		{"gate kind outside the run scope", timeScope(timeRun, "matter.agent_note"), agentP(timeRun, grant), record, one, agenttools.ErrGateKindNotAllowed, 0},
		{"no entries", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record, `{"entries":[]}`, agenttools.ErrInvalidInput, 0},
		{"zero minutes", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record, timeInput(timeEntry(timeMatter, "2026-10-09", 0)), agenttools.ErrInvalidInput, 0},
		// rule: LSO-3.6
		{"invented minutes", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record,
			timeInput(timeEntry(timeMatter, "2026-10-09", 45)), agenttools.ErrUnsourcedValue, 0},
		{"invented work date", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record,
			timeInput(timeEntry(timeMatter, "2026-10-08", 30)), agenttools.ErrUnsourcedValue, 0},
		{"manual path not checked", context.Background(), user, record, timeInput(timeEntry(timeMatter, "2026-10-08", 45)), nil, 1},
		{"entry for another user", timeScope(timeRun, agent.GateTimeAccept), agentP(timeRun, grant), record,
			timeInput(`{"matter_id":"` + timeMatter + `","work_date":"2026-10-09","minutes":30,"narrative":"x","user_id":"someone"}`), agenttools.ErrInvalidInput, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newTimeWorld(t, tc.actorCaps)
			res, err := w.reg.Call(tc.ctx, tc.p, agent.ToolProposeTimeEntries, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			g, err := w.gates.Get(tc.ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			var payload struct {
				Entries []struct {
					EntryID string `json:"entry_id"`
				} `json:"entries"`
			}
			if err := json.Unmarshal(g.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if g.Kind != agent.GateTimeAccept || g.Status != gate.StatusPending || len(payload.Entries) != tc.wantN {
				t.Fatalf("gate = %+v, payload %+v", g, payload)
			}
			for _, pe := range payload.Entries {
				e, err := w.entries.Get(tc.ctx, tk.EntryID(pe.EntryID))
				if err != nil {
					t.Fatalf("entry: %v", err)
				}
				// The entry is suggested, for the acting user only.
				if e.State != tk.StateSuggested || e.UserID != clerk || e.Origin != tk.OriginSuggestion {
					t.Errorf("entry = %+v, want suggested for %s", e, clerk)
				}
			}
		})
	}
}

// rule: ARC-I-039
// The accept gate is never decided by an agent principal: it stays pending.
func TestARC_I_039_ProposeTimeEntriesGateNotDecidedByAgent(t *testing.T) {
	cases := []struct {
		name string
		by   registry.Principal
	}{
		{"agent run accepts its suggestions", registry.Principal{Kind: registry.PrincipalAgent, RunID: timeRun, ActingFor: clerk, Grant: []string{timeCapName}}},
		{"another agent run accepts", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-other", ActingFor: clerk, Grant: []string{timeCapName}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := timeScope(timeRun, agent.GateTimeAccept)
			w := newTimeWorld(t, map[string]bool{timeCapName: true})
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: timeRun, ActingFor: clerk, Grant: []string{timeCapName}}
			res, err := w.reg.Call(ctx, p, agent.ToolProposeTimeEntries, json.RawMessage(timeInput(timeEntry(timeMatter, "2026-10-09", 30))))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			g0, err := w.gates.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			d := gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g0.PayloadHash, MFAVerifiedAt: time.Date(2026, 10, 11, 8, 59, 0, 0, time.UTC)}
			err = agenttools.DecideGuard{Engine: w.gates}.Decide(ctx, nil, g0.ID, g0.Version, d, tc.by)
			g, gerr := w.gates.Get(ctx, g0.ID)
			if gerr != nil {
				t.Fatalf("gate: %v", gerr)
			}
			if !errors.Is(err, agenttools.ErrAgentDecides) || g.Status != gate.StatusPending {
				t.Fatalf("decide err = %v status %s, want ErrAgentDecides and pending", err, g.Status)
			}
		})
	}
}
