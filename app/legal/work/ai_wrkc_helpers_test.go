package work_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/go-chi/chi/v5"
)

// Shared test helpers of the work screen-action and read-only AI features. Every name carries
// the wrkc prefix; the run helpers of the wrk prefix (seeding, scripts, gate checks) are reused.

// wrkcCheckScreenDef fails t unless the screen-action Def d matches its spec/ai/features.json
// entry: its one action is the spec's "on request:" trigger on the spec's one screen, and a
// feature Set takes it once with a run scope of exactly its gate kinds.
func wrkcCheckScreenDef(t *testing.T, d feature.Def) {
	t.Helper()
	sf := wrkSpec(t, d.ID)
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	kind := d.Kind
	if kind == "" {
		kind = ai.KindToolUse
	}
	action, _ := strings.CutPrefix(sf.Trigger, "on request: ")
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"area", d.Area, "work"},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"actions", sorted(d.Actions), sorted(append([]string{action}, sf.OnRequest...))},
		{"tools", sorted(d.Tools), sorted(sf.Tools)},
		{"gate kinds", sorted(d.GateKinds), sorted(sf.Gate.Kind)},
		{"model call", string(kind), sf.ModelCall},
		{"fixture", d.Fixture, sf.Fixture},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, spec %q", tt.field, tt.got, tt.want)
			}
		})
	}
	s := feature.NewSet()
	if err := s.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(d); !errors.Is(err, feature.ErrBadDef) {
		t.Fatalf("second Add = %v, want ErrBadDef", err)
	}
	rs, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	// rule: AI-R-003
	// The run scope lets the run open only the Def's gate kinds.
	if !slices.Equal(rs.GateKinds, d.GateKinds) {
		t.Fatalf("run gate kinds = %v, want %v", rs.GateKinds, d.GateKinds)
	}
}

// wrkcAccess lets the user read every record and hold every capability.
type wrkcAccess struct{}

func (wrkcAccess) Can(context.Context, registry.Principal, string, registry.Subject) (bool, error) {
	return true, nil
}

func (wrkcAccess) AgentPrincipal(_ context.Context, r identity.RunID, u identity.UserID, g []string) (registry.Principal, error) {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: string(r), ActingFor: string(u), Grant: g}, nil
}

// wrkcQueue records the runs the screen-action route enqueues.
type wrkcQueue struct{ runs []string }

func (q *wrkcQueue) EnqueueAction(_ context.Context, featureID string, subjects []trigger.SubjectRef, actingFor string) (string, error) {
	q.runs = append(q.runs, featureID+"|"+actingFor+"|"+subjects[0].ID)
	return "run-wrkc", nil
}

// wrkcCheckAction fails t unless the screen-action route queues the write feature d for the
// acting user when started from action's screen (which selects records of subjectKind), and
// refuses it from any other screen.
func wrkcCheckAction(t *testing.T, d feature.Def, action, subjectKind, capability string) {
	t.Helper()
	if !slices.Equal(d.Actions, []string{action}) {
		t.Fatalf("actions = %v, want [%s]", d.Actions, action)
	}
	screen, _, _ := strings.Cut(action, " > ")
	set := feature.NewSet()
	if err := set.Add(d); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		screen string
		status int
		queued []string
	}{
		{"own screen queues the run", screen, http.StatusAccepted, []string{d.ID + "|" + agenttest.FeatureActingFor + "|rec-1"}},
		{"other screen refused", "matter-detail", http.StatusUnprocessableEntity, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &wrkcQueue{}
			a := &trigger.Actions{
				Features: set,
				Screens: map[string]trigger.Screen{
					screen:          {SubjectKind: subjectKind, Capability: capability},
					"matter-detail": {SubjectKind: "matter", Capability: "matters.view"},
				},
				Access: wrkcAccess{}, Queue: q,
				NewRunID: func() string { return "run-wrkc" },
				Principal: func(*http.Request) (registry.Principal, bool) {
					return registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, true
				},
				Problems: &server.Problems{},
			}
			r := chi.NewRouter()
			a.Routes(r)
			rec := httptest.NewRecorder()
			body := `{"screen":"` + tt.screen + `","record_ids":["rec-1"]}`
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/agent/actions/"+d.ID, bytes.NewBufferString(body)))
			if rec.Code != tt.status || !slices.Equal(q.runs, tt.queued) {
				t.Fatalf("status = %d, queued %v; want %d, %v (body %s)", rec.Code, q.runs, tt.status, tt.queued, rec.Body)
			}
		})
	}
}
