package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	pbProdMatter = "0190a5c3-0000-7000-8000-0000000000b1"
	pbProdUser   = "0190a5c3-0000-7000-8000-0000000000b2"
)

// closingMatter is a matter.Query holding one matter.
type closingMatter struct{}

func (closingMatter) Get(_ context.Context, id matter.ID) (matter.Matter, error) {
	if id != pbProdMatter {
		return matter.Matter{}, agenttools.ErrNotFound
	}
	return matter.Matter{ID: id, Status: "open"}, nil
}
func (closingMatter) List(context.Context, matter.Filter) ([]matter.Matter, error) { return nil, nil }
func (closingMatter) Members(context.Context, matter.ID) ([]matter.Member, error)  { return nil, nil }

// signedBalance is a MatterTrustBalances holding one signed balance.
type signedBalance int64

func (b signedBalance) MatterTrustBalance(context.Context, string) (int64, error) {
	return int64(b), nil
}

// matterOfMatter resolves a matter subject to itself.
type matterOfMatter struct{}

func (matterOfMatter) SubjectMatter(_ context.Context, _, id string) (string, error) { return id, nil }

// openWalls shows every matter.
type openWalls struct{}

func (openWalls) Visible(context.Context, string, string) (bool, error) { return true, nil }

// openedGate records the gate the opener was asked to open.
type openedGate struct {
	kind    string
	payload map[string]any
}

// TestProductionSubmitBuildsPayload: production's submit_for_approval builds the gate payload
// with the payload builders boot wires (PGSubjectRecords over the matter port, the trust
// balance port), and an overdrawn matter's G13 carries the By-law 9 blocker.
func TestProductionSubmitBuildsPayload(t *testing.T) {
	tests := []struct {
		name          string
		balance       int64
		wantBalance   string
		wantOverdrawn bool
		wantShortfall string
		wantBlocker   bool
	}{
		{"funded matter: balance, no blocker", 12_500, "12500", false, "0", false},
		// rule: BIL-I-010
		{"overdrawn matter: shortfall and trust_overdrawn blocker", -4_000, "0", true, "4000", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got openedGate
			w := noLawyerWiring()
			w.Ports.Can = func(context.Context, string, string) (bool, error) { return true, nil }
			w.Ports.Walls = openWalls{}
			w.Ports.Matters = closingMatter{}
			w.Ports.SubjectMatters = matterOfMatter{}
			w.Ports.TrustBalances = signedBalance(tc.balance)
			w.Ports.OpenGate = func(_ context.Context, kind string, _ registry.Subject, payload json.RawMessage, _ registry.Principal) (gate.GateID, error) {
				got.kind = kind
				dec := json.NewDecoder(bytes.NewReader(payload))
				dec.UseNumber()
				if err := dec.Decode(&got.payload); err != nil {
					t.Fatalf("payload: %v", err)
				}
				return "gate-1", nil
			}
			c := compose(t, legalFlagNames(t), Options{Production: mockProduction(&AgentReadiness{}), Agent: w})
			if w.Ports.PayloadBuilders == nil {
				t.Fatal("production wired no payload builders")
			}
			var submit *registry.Tool
			for _, call := range c.boot.Method(registry.MethodTool) {
				if tl := call.Args[0].(registry.Tool); tl.Name == agent.ToolSubmitForApproval {
					submit = &tl
				}
			}
			if submit == nil {
				t.Fatal("submit_for_approval not served")
			}
			in, _ := json.Marshal(map[string]string{"subject_type": "matter", "subject_id": pbProdMatter, "gate_kind": agent.GateClosing})
			p := registry.Principal{Kind: registry.PrincipalUser, UserID: pbProdUser}
			if _, err := submit.Handle(context.Background(), p, in); err != nil {
				t.Fatalf("submit: %v", err)
			}
			if got.kind != agent.GateClosing || got.payload["matter_id"] != pbProdMatter {
				t.Fatalf("opened %s %v", got.kind, got.payload)
			}
			if got.payload["trust_balance_cents"] != json.Number(tc.wantBalance) || got.payload["overdrawn"] != tc.wantOverdrawn ||
				got.payload["shortfall_cents"] != json.Number(tc.wantShortfall) {
				t.Fatalf("payload %v", got.payload)
			}
			blockers, _ := got.payload["blockers"].([]any)
			hasBlocker := len(blockers) == 1 && blockers[0] == agent.BlockerTrustOverdrawn
			if hasBlocker != tc.wantBlocker {
				t.Fatalf("blockers %v, want trust_overdrawn %v", blockers, tc.wantBlocker)
			}
		})
	}
}
