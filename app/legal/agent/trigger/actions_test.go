package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// fakeAccess: walled records are hidden, and the user holds only caps.
type fakeAccess struct {
	walled map[string]bool
	caps   map[string]bool
	checks []string
}

func (f *fakeAccess) Can(_ context.Context, _ registry.Principal, c string, s registry.Subject) (bool, error) {
	f.checks = append(f.checks, c+"@"+s.ID)
	if f.walled[s.ID] {
		return false, identity.ErrNotVisible
	}
	return f.caps[c], nil
}

func (f *fakeAccess) AgentPrincipal(_ context.Context, r identity.RunID, u identity.UserID, g []string) (registry.Principal, error) {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: string(r), ActingFor: string(u), Grant: g}, nil
}

type fakeRunner struct {
	out    run.Outcome
	calls  int
	prompt string
	actor  registry.Principal
}

func (f *fakeRunner) Run(_ context.Context, _ run.Spec, p run.Principal, prompt string, _ ...run.Trigger) (run.Outcome, error) {
	f.calls++
	f.prompt, f.actor = prompt, p
	return f.out, nil
}

type fakeActionQueue struct {
	runs []string
}

func (f *fakeActionQueue) EnqueueAction(_ context.Context, featureID string, subjects []SubjectRef, actingFor string) (string, error) {
	f.runs = append(f.runs, featureID+"|"+actingFor+"|"+subjects[0].ID)
	return "run-q", nil
}

func actionDef(id string, kind ai.CallKind, gates []string, tools []string) feature.Def {
	return feature.Def{
		ID: id, Source: feature.SourceScreenAction, Kind: kind, Trigger: "on request: matter-detail > Ask",
		Actions: []string{"matter-detail > Ask"}, Tools: tools, GateKinds: gates, Grant: []string{"matter.read"},
		Prompt: fstest.MapFS{"p.md": {Data: []byte("prompt")}}, PromptPath: "p.md",
	}
}

func TestScreenAction(t *testing.T) {
	cite := agenttools.Citation{VersionID: "v-1", Page: 2, FromLine: 1, ToLine: 9}
	answer := run.Outcome{
		JSON:      json.RawMessage(`{"answer":"The limitation date is cited.","citations":[{"version_id":"v-1","page":2,"from_line":3,"to_line":4}]}`),
		Citations: []agenttools.Citation{cite},
	}
	uncited := run.Outcome{
		JSON:      json.RawMessage(`{"answer":"Made up.","citations":[{"version_id":"v-9","page":1,"from_line":1,"to_line":1}]}`),
		Citations: []agenttools.Citation{cite},
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-1"}
	cases := []struct {
		name       string
		feature    string
		body       string
		who        *registry.Principal
		caps       []string
		walled     []string
		out        run.Outcome
		wantStatus int
		wantRuns   int // inline runner calls
		wantQueued int
		wantBody   string
	}{
		{"chat answers inline with citations", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1","m-2"]}`, &user,
			[]string{"matter.view", "matter.read"}, nil, answer, http.StatusOK, 1, 0, `"version_id":"v-1"`},
		{"write feature queued", "F-WRITE", `{"screen":"matter-detail","record_ids":["m-1"]}`, &user,
			[]string{"matter.view", "matter.read"}, nil, answer, http.StatusAccepted, 0, 1, `"run_id":"run-q"`},
		// rule: ARC-I-036
		{"walled record is 404", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1","m-walled"]}`, &user,
			[]string{"matter.view", "matter.read"}, []string{"m-walled"}, answer, http.StatusNotFound, 0, 0, `"not_found"`},
		// rule: ARC-I-036
		{"missing grant capability is 403", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1"]}`, &user,
			[]string{"matter.view"}, nil, answer, http.StatusForbidden, 0, 0, `"missing_capability"`},
		{"agent principal cannot start", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1"]}`,
			&registry.Principal{Kind: registry.PrincipalAgent, RunID: "r", ActingFor: "u-1"},
			[]string{"matter.view", "matter.read"}, nil, answer, http.StatusForbidden, 0, 0, `"forbidden"`},
		{"unauthenticated", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1"]}`, nil,
			nil, nil, answer, http.StatusUnauthorized, 0, 0, ""},
		{"unknown feature", "F-NONE", `{"screen":"matter-detail","record_ids":["m-1"]}`, &user,
			nil, nil, answer, http.StatusNotFound, 0, 0, ""},
		{"screen not an action of the feature", "F-CHAT", `{"screen":"bill-detail","record_ids":["m-1"]}`, &user,
			nil, nil, answer, http.StatusUnprocessableEntity, 0, 0, ""},
		{"no record ids", "F-CHAT", `{"screen":"matter-detail","record_ids":[]}`, &user,
			nil, nil, answer, http.StatusUnprocessableEntity, 0, 0, ""},
		{"free text field refused", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1"],"note":"client said"}`, &user,
			nil, nil, answer, http.StatusBadRequest, 0, 0, ""},
		// rule: AI-R-005
		{"answer citing another run refused", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1"]}`, &user,
			[]string{"matter.view", "matter.read"}, nil, uncited, http.StatusInternalServerError, 1, 0, `"internal_error"`},
		{"model busy", "F-CHAT", `{"screen":"matter-detail","record_ids":["m-1"]}`, &user,
			[]string{"matter.view", "matter.read"}, nil, run.Outcome{RetryQueued: true}, http.StatusServiceUnavailable, 1, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := feature.NewSet()
			for _, d := range []feature.Def{
				actionDef("F-CHAT", ai.KindChat, nil, nil),
				actionDef("F-WRITE", ai.KindToolUse, []string{"docs.outgoing"}, nil),
			} {
				if err := set.Add(d); err != nil {
					t.Fatal(err)
				}
			}
			acc := &fakeAccess{walled: map[string]bool{}, caps: map[string]bool{}}
			for _, w := range c.walled {
				acc.walled[w] = true
			}
			for _, cp := range c.caps {
				acc.caps[cp] = true
			}
			rn := &fakeRunner{out: c.out}
			q := &fakeActionQueue{}
			a := &Actions{
				Features: set,
				Screens:  map[string]Screen{"matter-detail": {SubjectKind: "matter", Capability: "matter.view"}, "bill-detail": {SubjectKind: "bill", Capability: "billing.view"}},
				Access:   acc, Runner: rn, Queue: q,
				NewRunID: func() string { return "run-1" },
				Principal: func(*http.Request) (registry.Principal, bool) {
					if c.who == nil {
						return registry.Principal{}, false
					}
					return *c.who, true
				},
				Problems: &server.Problems{},
			}
			r := chi.NewRouter()
			a.Routes(r)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/agent/actions/"+c.feature, bytes.NewBufferString(c.body))
			r.ServeHTTP(rec, req)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, c.wantStatus, rec.Body)
			}
			if rn.calls != c.wantRuns || len(q.runs) != c.wantQueued {
				t.Fatalf("runs = %d queued = %d, want %d and %d", rn.calls, len(q.runs), c.wantRuns, c.wantQueued)
			}
			if c.wantBody != "" && !strings.Contains(rec.Body.String(), c.wantBody) {
				t.Fatalf("body %s lacks %s", rec.Body, c.wantBody)
			}
			if rn.calls > 0 {
				// rule: ARC-I-036
				// The run acts as an agent for the requesting user; its prompt holds ids only.
				if rn.actor.Kind != registry.PrincipalAgent || rn.actor.ActingFor != "u-1" {
					t.Fatalf("run principal = %+v", rn.actor)
				}
				if rn.prompt != "screen: matter-detail\nrecords: matter:m-1" && !strings.HasPrefix(rn.prompt, "screen: matter-detail\nrecords: matter:m-1, matter:m-2") {
					t.Fatalf("prompt = %q", rn.prompt)
				}
			}
			if c.wantStatus == http.StatusOK || c.wantStatus == http.StatusAccepted {
				// Every record is checked for the screen capability and each grant.
				for _, want := range []string{"matter.view@m-1", "matter.read@m-1"} {
					if !strings.Contains(strings.Join(acc.checks, ","), want) {
						t.Fatalf("checks %v lack %s", acc.checks, want)
					}
				}
			}
		})
	}
}

func TestOnScreen(t *testing.T) {
	d := feature.Def{Actions: []string{"matter-detail > Ask", " roles-list >Explain"}}
	cases := []struct {
		screen string
		want   bool
	}{{"matter-detail", true}, {"roles-list", true}, {"matter", false}, {"", false}}
	for _, c := range cases {
		if got := onScreen(d, c.screen); got != c.want {
			t.Errorf("onScreen(%q) = %v", c.screen, got)
		}
	}
}
