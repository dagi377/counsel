package boot

import (
	"context"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/registry"
)

// noJobs accepts every insert and runs nothing.
type noJobs struct{}

func (noJobs) Insert(context.Context, river.JobArgs, *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	return &rivertype.JobInsertResult{}, nil
}

// mockProduction is the production config over the in-process mock model.
func mockProduction(ready *AgentReadiness) *Production {
	key := trust.SealKey{ID: 1, Key: make([]byte, 32)}
	return &Production{SealKey: &key, ModelClient: aifake.New(fstest.MapFS{}), FirmAccount: "firm-general",
		Jobs: noJobs{}, Readiness: ready}
}

func TestProductionAgentReadiness(t *testing.T) {
	key := trust.SealKey{ID: 1, Key: make([]byte, 32)}
	features, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		prod       Production
		wantReason string // every feature unready for this reason; empty: none for no_model
	}{
		{"no model config outside dev is not ready, boot still runs", Production{SealKey: &key, FirmAccount: "firm-general"}, ReasonNoModel},
		{"live model in a test environment is not ready", Production{SealKey: &key, FirmAccount: "firm-general",
			Model: &ModelConfig{Mode: "live", BaseURL: "http://model.invalid", KeyRef: "K"}, ModelEnv: "test"}, ReasonNoModel},
		{"no firm account is not ready", Production{SealKey: &key, ModelClient: aifake.New(fstest.MapFS{})}, ReasonNoFirmAccount},
		{"mock model wires the agent", *mockProduction(nil), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready := &AgentReadiness{}
			p := tc.prod
			p.Readiness = ready
			c := compose(t, legalFlagNames(t), Options{Production: &p, Agent: noLawyerWiring()})
			got, err := ready.Unready(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			tools := c.boot.Method(registry.MethodTool)
			if tc.wantReason == "" {
				if len(tools) == 0 {
					t.Fatal("no agent tool registered")
				}
				for _, u := range got {
					if u.Reason == ReasonNoModel || u.Reason == ReasonNoFirmAccount {
						t.Fatalf("unready %v", u)
					}
				}
				return
			}
			if len(tools) != 0 {
				t.Fatalf("%d agent tools registered without a model", len(tools))
			}
			for _, d := range features.All() {
				if !slices.Contains(got, trigger.Unready{FeatureID: d.ID, Reason: tc.wantReason}) {
					t.Errorf("%s not reported %s", d.ID, tc.wantReason)
				}
			}
		})
	}
}

// TestProductionAgentGraph builds Production over a pool that never connects and the mock
// model: every feature resolves its tools (served, or reported not ready with the port that
// has no implementation), its gate kinds and its job binding, and the action route is mounted.
func TestProductionAgentGraph(t *testing.T) {
	ready := &AgentReadiness{}
	c := compose(t, legalFlagNames(t), Options{Production: mockProduction(ready), Agent: noLawyerWiring()})
	features, err := Features()
	if err != nil {
		t.Fatalf("features: %v", err)
	}
	var served []string
	for _, call := range c.boot.Method(registry.MethodTool) {
		served = append(served, call.Args[0].(registry.Tool).Name)
	}
	unready, err := ready.Unready(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reported := func(id string) bool {
		return slices.ContainsFunc(unready, func(u trigger.Unready) bool { return u.FeatureID == id && u.Reason == ReasonToolUnwired })
	}
	handlers, err := ServedToolNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range features.All() {
		for _, tl := range d.Tools {
			if !slices.Contains(handlers, tl) {
				t.Errorf("%s: tool %s has no handler", d.ID, tl)
			}
			if isHandler(tl) && !slices.Contains(served, tl) && !reported(d.ID) {
				t.Errorf("%s: tool %s neither served nor reported not ready", d.ID, tl)
			}
		}
	}
	// rule: ARC-I-040
	// The job bindings were added: a scheduled feature with no designated lawyer is unready.
	if !slices.ContainsFunc(unready, func(u trigger.Unready) bool { return u.Reason == trigger.ReasonNoDesignatedLawyer }) {
		t.Error("no scheduled binding reported: job bindings not wired")
	}
	var actions bool
	for _, r := range c.server.Routes() {
		if r.Method == "POST" && r.Pattern == "/agent/actions/{feature_id}" {
			actions = true
		}
	}
	if !actions {
		t.Fatal("POST /agent/actions/{feature_id} is not mounted")
	}
}

func isHandler(name string) bool {
	_, ok := agent.Handlers().For(name)
	return ok
}

// noLawyer names no designated lawyer for any feature.
type noLawyer struct{}

func (noLawyer) DesignatedLawyer(context.Context, string) (string, error) { return "", nil }

// noLawyerWiring is an agent wiring whose firm settings name no designated lawyer; boot
// fills every other port.
func noLawyerWiring() *AgentWiring { return &AgentWiring{Designated: noLawyer{}} }

// TestProductionAgentToolPortsReady: production wires Text, Documents, Entries, DraftCalc,
// DraftStore and BatchDrafts over Postgres, so the tools reading them are served and no
// feature is reported not ready for an unwired tool.
func TestProductionAgentToolPortsReady(t *testing.T) {
	ready := &AgentReadiness{}
	w := noLawyerWiring()
	c := compose(t, legalFlagNames(t), Options{Production: mockProduction(ready), Agent: w})
	var served []string
	for _, call := range c.boot.Method(registry.MethodTool) {
		served = append(served, call.Args[0].(registry.Tool).Name)
	}
	unready, err := ready.Unready(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range unready {
		if u.Reason == ReasonToolUnwired {
			t.Errorf("%s not ready: a tool port has no implementation", u.FeatureID)
		}
	}
	missing := w.Ports.Missing()
	tests := []struct {
		port string
		tool string
	}{
		{"Text", agent.ToolSearchDocuments},
		{"Documents", agent.ToolListDocuments},
		{"Entries", agent.ToolProposeTimeEntries},
		{"DraftCalc", agent.ToolDraftBill},
		{"DraftStore", agent.ToolDraftBill},
		{"BatchDrafts", agent.ToolDraftBill},
	}
	for _, tc := range tests {
		t.Run(tc.port, func(t *testing.T) {
			if slices.Contains(missing, tc.port) {
				t.Errorf("port %s not wired", tc.port)
			}
			if !slices.Contains(served, tc.tool) {
				t.Errorf("tool %s not served", tc.tool)
			}
		})
	}
}

// aLawyer names one designated lawyer for every feature.
type aLawyer struct{}

func (aLawyer) DesignatedLawyer(context.Context, string) (string, error) {
	return "0190a5c3-0000-7000-8000-0000000000aa", nil
}

// TestProductionEveryAgentFeatureReady: with the mock model, a firm account, a job queue,
// a designated lawyer and a configured document bucket, production reports every agent
// feature ready: no tool has an unwired port, and the payload builders, the trust transfer
// port and the gate opener are wired.
func TestProductionEveryAgentFeatureReady(t *testing.T) {
	ready := &AgentReadiness{}
	p := mockProduction(ready)
	p.Docs = DocsBucketConfig{URL: "https://s3.example/cell-docs", Region: "ca-central-1",
		AccessKey: "AKIDEXAMPLE", SecretKey: "secret-example"}
	w := &AgentWiring{Designated: aLawyer{}}
	c := compose(t, legalFlagNames(t), Options{Production: p, Agent: w})
	unready, err := ready.Unready(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range unready {
		t.Errorf("%s not ready: %s", u.FeatureID, u.Reason)
	}
	missing := w.Ports.Missing()
	for _, port := range []string{"PayloadBuilders", "TrustTransfers", "OpenGate", "SubjectMatters"} {
		t.Run(port, func(t *testing.T) {
			if slices.Contains(missing, port) {
				t.Errorf("port %s not wired", port)
			}
		})
	}
	var upload bool
	for _, r := range c.server.Routes() {
		if r.Method == "POST" && r.Pattern == "/documents" {
			upload = true
		}
	}
	if !upload {
		t.Error("POST /documents not mounted with a configured bucket")
	}
}
