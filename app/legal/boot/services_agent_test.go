package boot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/webhook"
)

// agentAccess allows everything and builds agent principals.
type agentAccess struct{ allowAll }

func (agentAccess) AgentPrincipal(_ context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (registry.Principal, error) {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: string(run), ActingFor: string(actingFor), Grant: grant}, nil
}

const hookID = "0190c000-0000-7000-8000-000000000001"

// oneEndpoint is a webhook EndpointReads and ExpiryStore holding one endpoint at version 3.
type oneEndpoint struct{}

func (oneEndpoint) Endpoints(context.Context, webhook.EndpointFilter) ([]webhook.EndpointInfo, error) {
	return nil, nil
}
func (oneEndpoint) Endpoint(_ context.Context, id string) (webhook.EndpointInfo, error) {
	if id != hookID {
		return webhook.EndpointInfo{}, webhook.ErrUnknownEndpoint
	}
	return webhook.EndpointInfo{ID: hookID, URL: "https://example.test/a", Status: webhook.StatusActive, Version: 3}, nil
}
func (oneEndpoint) Expiring(context.Context, time.Time) ([]webhook.EndpointExpiry, error) {
	return nil, nil
}
func (oneEndpoint) ClaimNotice(context.Context, string, time.Time, time.Duration) (bool, error) {
	return false, nil
}
func (oneEndpoint) SetExpiry(context.Context, string, *time.Time) error { return nil }

func agentPorts() *HTTPPorts {
	p := sharedPorts()
	p.Access = agentAccess{}
	p.DB = nilTx{}
	p.AgentModel = aifake.New(fstest.MapFS{})
	p.AgentTools = agenttoolsfake.New()
	p.WebhookEndpoints = oneEndpoint{}
	return p
}

func TestWithAgentRuns(t *testing.T) {
	tests := []struct {
		name     string
		ports    func() *HTTPPorts
		kernel   bool
		wantRuns bool
	}{
		{"kernel DB, model and tools build the run starter", agentPorts, true, true},
		{"no kernel DB keeps runs unset", agentPorts, false, false},
		{"no model keeps runs unset", func() *HTTPPorts { p := agentPorts(); p.AgentModel = nil; return p }, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := pgKernel()
			if !tt.kernel {
				k.DB = nil
			}
			p, err := tt.ports().withAgentRuns(k)
			if err != nil {
				t.Fatal(err)
			}
			if got := is[*agent.RunStarter](p.Runs); got != tt.wantRuns {
				t.Fatalf("Runs = %T, want RunStarter %v", p.Runs, tt.wantRuns)
			}
			if !is[agentAccess](p.RunAccess) {
				t.Fatalf("RunAccess = %T, want the shared Access", p.RunAccess)
			}
		})
	}
}

// TestAgentRunAndWebhookRoutesServe: with a kernel DB, the HTTP-25 run routes and the HTTP-26
// endpoint routes are wired and no longer answer 503; PATCH enforces If-Match.
func TestAgentRunAndWebhookRoutesServe(t *testing.T) {
	rec := &registry.Recorder{}
	k := pgKernel()
	k.WebhookExpiry = oneEndpoint{}
	k.Registry = recorderBooter{rec}
	if err := App(Options{HTTP: agentPorts()}).Register(context.Background(), k); err != nil {
		t.Fatalf("Register: %v", err)
	}
	router, seen := served(t, rec.Calls())
	for route, n := range seen {
		if n > 1 {
			t.Errorf("%s mounted %d times; the placeholder was not replaced", route, n)
		}
	}
	tests := []struct {
		method, path, ifMatch string
		want                  int
	}{
		{http.MethodPost, "/agent/runs", "", http.StatusBadRequest},
		{http.MethodGet, "/agent/runs/not-a-uuid", "", http.StatusBadRequest},
		{http.MethodGet, "/webhook-endpoints", "", http.StatusOK},
		// rule: ARC-I-006
		{http.MethodPatch, "/webhook-endpoints/" + hookID, "", http.StatusPreconditionRequired},
		{http.MethodPatch, "/webhook-endpoints/" + hookID, `"2"`, http.StatusPreconditionFailed},
		{http.MethodPatch, "/webhook-endpoints/" + hookID, `"3"`, http.StatusOK},
	}
	for i, tt := range tests {
		t.Run(tt.method+" "+tt.path+" "+tt.ifMatch, func(t *testing.T) {
			if !seenPath(seen, tt.method, tt.path) {
				t.Fatal("route not mounted")
			}
			body := "{}"
			if tt.method == http.MethodPatch {
				body = `{"expires_at":"2027-01-01T00:00:00Z"}`
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", fmt.Sprintf("key-%08d", i))
			if tt.ifMatch != "" {
				req.Header.Set("If-Match", tt.ifMatch)
			}
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

// triggerSeenRunner checks, while the run is in flight, whether its trigger date counts as read.
type triggerSeenRunner struct {
	pv   *agenttools.Provenance
	seen *bool
}

func (r triggerSeenRunner) Run(_ context.Context, _ agentrun.Spec, p agentrun.Principal, _ string, _ ...agentrun.Trigger) (agentrun.Outcome, error) {
	*r.seen = r.pv.Check(p.RunID, []string{"/period_to"}, []byte(`{"period_to":"2026-09-30"}`)) == nil
	return agentrun.Outcome{}, nil
}

// TestWithAgentRunsRecordsTriggers: a run the HTTP-25 RunStarter drives records its inputs
// in the agent provenance, as the boot runner records its triggers, and drops them at the end.
func TestWithAgentRunsRecordsTriggers(t *testing.T) {
	tests := []struct {
		name       string
		provenance bool
		wantSeen   bool
	}{
		{"provenance wired: inputs count as read", true, true},
		{"no provenance: no wrap", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := agentPorts()
			pv := agenttools.NewProvenance()
			if tt.provenance {
				p.AgentProvenance = pv
			}
			got, err := p.withAgentRuns(pgKernel())
			if err != nil {
				t.Fatal(err)
			}
			rs := got.Runs.(*agent.RunStarter)
			if (rs.Wrap != nil) != tt.provenance {
				t.Fatalf("Wrap set = %v", rs.Wrap != nil)
			}
			if rs.Wrap == nil {
				return
			}
			var seen bool
			// rule: ARC-U-027
			l := rs.Wrap(triggerSeenRunner{pv: pv, seen: &seen})
			pr := agentrun.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u"}
			if _, err := l.Run(context.Background(), agentrun.Spec{FeatureID: "F-BIL-01"}, pr, "skill", agentrun.Trigger{Text: `{"period_to":"2026-09-30"}`}); err != nil {
				t.Fatal(err)
			}
			if seen != tt.wantSeen {
				t.Fatalf("trigger value seen = %v", seen)
			}
			if pv.Check("run-1", []string{"/period_to"}, []byte(`{"period_to":"2026-09-30"}`)) == nil {
				t.Fatal("run values kept after the run ended")
			}
		})
	}
}
