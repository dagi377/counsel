package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	memberAsk   = "0190d000-0000-7000-8000-000000000001"
	memberOther = "0190d000-0000-7000-8000-000000000002"
	memberNone  = "0190d000-0000-7000-8000-000000000003"

	matterOpen   = "0190e000-0000-7000-8000-000000000001"
	matterWalled = "0190e000-0000-7000-8000-0000000000ff"

	runMine    = "0190a000-0000-7000-8000-000000000001"
	runOther   = "0190a000-0000-7000-8000-000000000002"
	runWalled  = "0190a000-0000-7000-8000-000000000003"
	runMissing = "0190a000-0000-7000-8000-0000000000ee"
)

// stubRuns is an in-memory agent.RunService that counts calls.
type stubRuns struct {
	mu     sync.Mutex
	runs   map[string]agent.AgentRun
	starts []agent.StartRun
	calls  int
}

func (s *stubRuns) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubRuns) SkillGrant(_ context.Context, skill string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if skill != "draft-letter" {
		return nil, agent.ErrUnknownSkill
	}
	return []string{"docs.draft", "billing.approve"}, nil
}

func (s *stubRuns) Start(_ context.Context, st agent.StartRun) (agent.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.starts = append(s.starts, st)
	run := agent.AgentRun{ID: st.ID, SkillID: st.SkillID, ActingFor: st.Principal.ActingFor, MatterID: st.MatterID,
		State: agent.RunQueued, Grant: st.Principal.Grant, CreatedAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)}
	s.runs[run.ID] = run
	return run, nil
}

func (s *stubRuns) Run(_ context.Context, id string) (agent.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	run, ok := s.runs[id]
	if !ok {
		return agent.AgentRun{}, fmt.Errorf("run %s: %w", id, agent.ErrRunNotFound)
	}
	return run, nil
}

// stubAccess grants per-member capabilities, walls matterWalled, and narrows an agent grant to
// what the member holds, never keeping an approve capability.
type stubAccess struct{ caps map[string][]string }

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == matterWalled {
		return false, identity.ErrNotVisible
	}
	return slices.Contains(a.caps[p.UserID], capability), nil
}

func (a stubAccess) AgentPrincipal(_ context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (registry.Principal, error) {
	var kept []string
	for _, g := range grant {
		if slices.Contains(a.caps[string(actingFor)], g) && g != "billing.approve" {
			kept = append(kept, g)
		}
	}
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: string(run), ActingFor: string(actingFor), Grant: kept}, nil
}

type rig struct {
	h    http.Handler
	runs *stubRuns
}

func newRig(t *testing.T) rig {
	t.Helper()
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	rg := rig{runs: &stubRuns{runs: map[string]agent.AgentRun{
		runMine:   {ID: runMine, SkillID: "draft-letter", ActingFor: memberAsk, State: agent.RunRunning, CreatedAt: at},
		runOther:  {ID: runOther, SkillID: "draft-letter", ActingFor: memberOther, State: agent.RunRunning, CreatedAt: at},
		runWalled: {ID: runWalled, SkillID: "draft-letter", ActingFor: memberAsk, MatterID: matterWalled, State: agent.RunRunning, CreatedAt: at},
	}}}
	seq := 0
	h := &agent.RunHTTP{
		Runs: rg.runs, Idem: idemfake.New(),
		Access: stubAccess{caps: map[string][]string{
			memberAsk:   {agent.CapAsk, "docs.draft", "billing.approve"},
			memberOther: {agent.CapAsk},
		}},
		Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		},
		NewID: func() string {
			seq++
			return fmt.Sprintf("0190f000-0000-7000-8000-%012d", seq)
		},
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	rg.h = m.Handler()
	return rg
}

var openAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
	raw, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		return nil, err
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	if err := c.AddResource("openapi.json", bytes.NewReader(js)); err != nil {
		return nil, err
	}
	return c, nil
})

func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, api.ProblemContentType)
	}
	c, err := openAPISchemas()
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("openapi.json#/components/schemas/Problem")
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("problem does not validate: %v", err)
	}
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code {
		t.Errorf("code = %q, want %q", p.Code, code)
	}
}

func do(h http.Handler, method, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestARC_I_051_AgentRunRoutesProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
	start := `{"skill_id":"draft-letter","inputs":{"tone":"formal"}}`
	tests := []struct {
		name      string
		method    string
		path      string
		user      string
		body      string
		hdr       map[string]string
		status    int
		code      string
		zeroCalls bool
	}{
		{"start without principal", "POST", "/v1/agent/runs", "", start, key, 401, agent.CodeUnauthenticated, true},
		{"get without principal", "GET", "/v1/agent/runs/" + runMine, "", "", nil, 401, agent.CodeUnauthenticated, true},
		{"start without agent.ask", "POST", "/v1/agent/runs", memberNone, start, key, 403, agent.CodeMissingCapability, true},
		{"get without agent.ask", "GET", "/v1/agent/runs/" + runMine, memberNone, "", nil, 403, agent.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"start on walled matter is 404, not 403", "POST", "/v1/agent/runs", memberAsk, `{"skill_id":"draft-letter","matter_id":"` + matterWalled + `"}`, key, 404, agent.CodeNotFound, true},
		{"get run on walled matter is 404", "GET", "/v1/agent/runs/" + runWalled, memberAsk, "", nil, 404, agent.CodeNotFound, false},
		{"get another member's run is 404", "GET", "/v1/agent/runs/" + runOther, memberAsk, "", nil, 404, agent.CodeNotFound, false},
		{"get unknown run", "GET", "/v1/agent/runs/" + runMissing, memberAsk, "", nil, 404, agent.CodeNotFound, false},
		{"get malformed run id", "GET", "/v1/agent/runs/nope", memberAsk, "", nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"start missing idempotency key", "POST", "/v1/agent/runs", memberAsk, start, nil, 400, idem.CodeKeyRequired, true},
		{"start unknown body field", "POST", "/v1/agent/runs", memberAsk, `{"skill_id":"draft-letter","model":"x"}`, key, 400, server.CodeValidationFailed, true},
		{"start malformed skill id", "POST", "/v1/agent/runs", memberAsk, `{"skill_id":"Draft Letter"}`, key, 400, server.CodeValidationFailed, true},
		{"start inputs not an object", "POST", "/v1/agent/runs", memberAsk, `{"skill_id":"draft-letter","inputs":[1]}`, key, 400, server.CodeValidationFailed, true},
		{"start malformed matter id", "POST", "/v1/agent/runs", memberAsk, `{"skill_id":"draft-letter","matter_id":"m1"}`, key, 400, server.CodeValidationFailed, true},
		{"start unknown skill", "POST", "/v1/agent/runs", memberAsk, `{"skill_id":"no-such-skill"}`, key, 422, agent.CodeUnknownSkill, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t)
			checkProblem(t, do(rg.h, tc.method, tc.path, tc.user, tc.body, tc.hdr), tc.status, tc.code)
			if tc.zeroCalls && rg.runs.Calls() != 0 {
				t.Errorf("service calls = %d, want 0", rg.runs.Calls())
			}
			if len(rg.runs.starts) != 0 {
				t.Errorf("runs started = %d, want 0", len(rg.runs.starts))
			}
		})
	}
}

func TestAgentRunRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&agent.RunHTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/agent/runs/"+runMine, memberAsk, "", nil), 503, agent.CodeUnavailable)
}

// rule: ARC-I-036
func TestARC_I_036_RunPrincipalIsIntersection(t *testing.T) {
	tests := []struct {
		name      string
		member    string
		wantGrant []string
	}{
		{"member holding the grant keeps it, minus approve", memberAsk, []string{"docs.draft"}},
		{"member without the grant gets none", memberOther, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t)
			rec := do(rg.h, "POST", "/v1/agent/runs", tc.member, `{"skill_id":"draft-letter","matter_id":"`+matterOpen+`"}`,
				map[string]string{idem.HeaderName: "key-00000002"})
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var v struct {
				ID        string   `json:"id"`
				ActingFor string   `json:"acting_for"`
				State     string   `json:"state"`
				Grant     []string `json:"grant"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
			if v.ActingFor != tc.member || v.State != agent.RunQueued || !slices.Equal(v.Grant, tc.wantGrant) {
				t.Fatalf("run = %+v, want acting for %s, queued, grant %v", v, tc.member, tc.wantGrant)
			}
			st := rg.runs.starts[0]
			if st.Principal.Kind != registry.PrincipalAgent || st.Principal.RunID != v.ID || st.MatterID != matterOpen {
				t.Fatalf("start = %+v", st)
			}
			got := do(rg.h, "GET", "/v1/agent/runs/"+v.ID, tc.member, "", nil)
			if got.Code != http.StatusOK {
				t.Fatalf("get status = %d (%s)", got.Code, got.Body)
			}
		})
	}
}

// rule: ARC-I-007
func TestARC_I_007_AgentRunIdempotency(t *testing.T) {
	rg := newRig(t)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	body := `{"skill_id":"draft-letter"}`
	first := do(rg.h, "POST", "/v1/agent/runs", memberAsk, body, hdr)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d (%s)", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", body, http.StatusAccepted, ""},
		// The kernel idem store answers 422 for a reused key with a different body (ARC-I-008).
		{"same key, different body", `{"skill_id":"draft-letter","inputs":{"a":1}}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(rg.h, "POST", "/v1/agent/runs", memberAsk, tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" {
				if rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true" {
					t.Errorf("replay body = %s, want %s", rec.Body, first.Body)
				}
				return
			}
			checkProblem(t, rec, tc.status, tc.code)
		})
	}
	if n := len(rg.runs.starts); n != 1 {
		t.Errorf("runs started = %d, want 1", n)
	}
}
