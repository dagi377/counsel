package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

type allowAll struct{}

func (allowAll) Can(context.Context, gate.Principal, string) (bool, error) { return true, nil }

func writeHandler(res agenttools.ToolResult) func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
	return func(context.Context, agenttools.Principal, json.RawMessage) (agenttools.ToolResult, error) {
		return res, nil
	}
}

func out(outcome, ref string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"outcome": outcome, "ref": ref})
	return b
}

// rule: ARC-I-035
func TestARC_I_035_WriteToolsDraftProposeOrOpenGateOnly(t *testing.T) {
	cases := []struct {
		name string
		res  agenttools.ToolResult
		ok   bool
	}{
		{"draft", agenttools.ToolResult{Output: out("draft", "d1")}, true},
		{"proposal", agenttools.ToolResult{Output: out("proposal", "p1")}, true},
		{"opened gate", agenttools.ToolResult{Output: out("gate", "g1"), GateID: "g1"}, true},
		{"final state", agenttools.ToolResult{Output: out("final", "r1")}, false},
		{"sent", agenttools.ToolResult{Output: out("sent", "m1")}, false},
		{"approved", agenttools.ToolResult{Output: out("approved", "g1"), GateID: "g1"}, false},
		{"gate without id", agenttools.ToolResult{Output: out("gate", "g1")}, false},
		{"gate id mismatch", agenttools.ToolResult{Output: out("gate", "g1"), GateID: "g2"}, false},
		{"draft carrying gate id", agenttools.ToolResult{Output: out("draft", "d1"), GateID: "g1"}, false},
		{"empty ref", agenttools.ToolResult{Output: out("draft", "")}, false},
		{"no outcome", agenttools.ToolResult{Output: json.RawMessage(`{"ok":true}`)}, false},
		{"not json", agenttools.ToolResult{Output: json.RawMessage(`nope`)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			plans := agenttoolsfake.New()
			if _, err := plans.ReportPlan(ctx, "r1", []string{"draft-record"}); err != nil {
				t.Fatalf("ReportPlan: %v", err)
			}
			w, err := agenttools.WriteTool(agenttools.Tool{Name: "draft_record", Kind: registry.ToolWrite, Input: []byte(subjectSchema), Handle: writeHandler(tc.res)})
			if err != nil {
				t.Fatalf("WriteTool: %v", err)
			}
			r := agenttools.NewRegistry(plans)
			if err := r.Register(w); err != nil {
				t.Fatalf("Register: %v", err)
			}
			for _, p := range []agenttools.Principal{agent("r1"), user()} {
				_, err := r.Call(ctx, p, "draft_record", json.RawMessage(`{"subject":"s1"}`))
				if tc.ok && err != nil {
					t.Fatalf("%s Call: %v", p.Kind, err)
				}
				if !tc.ok && !errors.Is(err, agenttools.ErrBadTool) {
					t.Fatalf("%s Call err = %v, want ErrBadTool", p.Kind, err)
				}
			}
		})
	}
}

// rule: ARC-I-035
func TestARC_I_035_WriteToolRejectsNonWriteKinds(t *testing.T) {
	cases := []struct {
		name string
		tool agenttools.Tool
	}{
		{"read", tool("get_subject", registry.ToolRead, "")},
		{"report", tool("report_note", registry.ToolReport, "")},
		{"no handler", agenttools.Tool{Name: "draft_x", Kind: registry.ToolWrite, Input: []byte(subjectSchema)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := agenttools.WriteTool(tc.tool); !errors.Is(err, agenttools.ErrBadTool) {
				t.Fatalf("WriteTool err = %v, want ErrBadTool", err)
			}
		})
	}
	if _, err := agenttools.OpenGateTool(nil, ""); !errors.Is(err, agenttools.ErrBadTool) {
		t.Fatalf("OpenGateTool(nil) err = %v, want ErrBadTool", err)
	}
}

const kindSlug = "records.publish"

func newEngine(t *testing.T) *gatefake.Fake {
	t.Helper()
	e := gatefake.New(func() time.Time { return time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC) }, allowAll{})
	err := e.Register(gate.GateKind{
		Slug: kindSlug, Approver: "records.approve", Risk: gate.RiskLow, MinApprovers: 1, TTL: 24 * time.Hour,
		PayloadSchema: []byte(`{"type":"object"}`),
		Effect:        func(context.Context, pgx.Tx, gate.Approved) error { return nil },
	})
	if err != nil {
		t.Fatalf("Register kind: %v", err)
	}
	return e
}

// rule: ARC-I-035
func TestARC_I_035_OpenGateToolOpensWithCallerAsPreparer(t *testing.T) {
	cases := []struct {
		name string
		p    agenttools.Principal
		in   string
		err  error
	}{
		{"agent opens", agent("r1", "records.open"), `{"kind":"records.publish","subject":{"kind":"record","id":"x1"},"payload":{"n":1}}`, nil},
		{"user opens", user(), `{"kind":"records.publish","subject":{"kind":"record","id":"x1"},"payload":{}}`, nil},
		{"decision field refused", agent("r1", "records.open"), `{"kind":"records.publish","subject":{"kind":"record","id":"x1"},"payload":{},"decision":"approve"}`, agenttools.ErrInvalidInput},
		{"agent without capability", agent("r1"), `{"kind":"records.publish","subject":{"kind":"record","id":"x1"},"payload":{}}`, agenttools.ErrMissingCapability},
		{"unknown gate kind", agent("r1", "records.open"), `{"kind":"records.nope","subject":{"kind":"record","id":"x1"},"payload":{}}`, gate.ErrUnknownKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEngine(t)
			plans := agenttoolsfake.New()
			if _, err := plans.ReportPlan(ctx, "r1", []string{"open-gate"}); err != nil {
				t.Fatalf("ReportPlan: %v", err)
			}
			open := func(ctx context.Context, kind string, s registry.Subject, payload json.RawMessage, by agenttools.Principal) (gate.GateID, error) {
				return e.Open(ctx, nil, kind, s, payload, by)
			}
			og, err := agenttools.OpenGateTool(open, "records.open")
			if err != nil {
				t.Fatalf("OpenGateTool: %v", err)
			}
			r := agenttools.NewRegistry(plans)
			if err := r.Register(og); err != nil {
				t.Fatalf("Register: %v", err)
			}
			res, err := r.Call(ctx, tc.p, agenttools.ToolOpenGate, json.RawMessage(tc.in))
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("Call err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			g, err := e.Get(ctx, gate.GateID(res.GateID))
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if g.Status != gate.StatusPending || g.Preparer.Kind != tc.p.Kind || g.Preparer.RunID != tc.p.RunID {
				t.Fatalf("gate = status %s preparer %+v, want pending with caller as preparer", g.Status, g.Preparer)
			}
		})
	}
}

// rule: ARC-I-017
func TestARC_I_017_AgentPrincipalDecidingAnyGateIsRefused(t *testing.T) {
	approve := gate.Decision{Verdict: gate.VerdictApprove, MFAVerifiedAt: time.Date(2026, 10, 11, 8, 59, 0, 0, time.UTC)}
	reject := gate.Decision{Verdict: gate.VerdictReject, Reason: "no", MFAVerifiedAt: approve.MFAVerifiedAt}
	cases := []struct {
		name    string
		by      agenttools.Principal
		d       gate.Decision
		refused bool
	}{
		{"agent approves", agent("r9", "records.approve"), approve, true},
		{"agent rejects", agent("r9", "records.approve"), reject, true},
		{"agent acting for another user", agenttools.Principal{Kind: registry.PrincipalAgent, RunID: "r9", ActingFor: "u2", Grant: []string{"records.approve"}}, approve, true},
		{"guest", agenttools.Principal{Kind: registry.PrincipalGuest, UserID: "g1"}, approve, true},
		{"user approves", agenttools.Principal{Kind: registry.PrincipalUser, UserID: "u2"}, approve, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEngine(t)
			id, err := e.Open(ctx, nil, kindSlug, registry.Subject{Kind: "record", ID: "x1"}, json.RawMessage(`{}`), user())
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			g0, err := e.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			tc.d.PayloadHash = g0.PayloadHash
			err = agenttools.DecideGuard{Engine: e}.Decide(ctx, nil, id, g0.Version, tc.d, tc.by)
			g, gerr := e.Get(ctx, id)
			if gerr != nil {
				t.Fatalf("Get: %v", gerr)
			}
			if tc.refused {
				if !errors.Is(err, agenttools.ErrAgentDecides) {
					t.Fatalf("Decide err = %v, want ErrAgentDecides", err)
				}
				if g.Status != gate.StatusPending {
					t.Fatalf("status = %s after refused decision, want pending", g.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if g.Status == gate.StatusPending {
				t.Fatalf("status still pending after a user approval")
			}
		})
	}
}

// rule: ARC-I-035
func TestARC_I_035_UnwrappedWriteToolRefused(t *testing.T) {
	cases := []struct {
		name string
		res  agenttools.ToolResult
		ok   bool
	}{
		{"unwrapped draft passes", agenttools.ToolResult{Output: out("draft", "d1")}, true},
		{"unwrapped send refused", agenttools.ToolResult{Output: out("sent", "m1")}, false},
		{"unwrapped approve refused", agenttools.ToolResult{Output: out("approved", "g1"), GateID: "g1"}, false},
		{"unwrapped free-form output refused", agenttools.ToolResult{Output: json.RawMessage(`{"ok":true}`)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			plans := agenttoolsfake.New()
			if _, err := plans.ReportPlan(ctx, "r1", []string{"draft-record"}); err != nil {
				t.Fatalf("ReportPlan: %v", err)
			}
			r := agenttools.NewRegistry(plans)
			// Registered directly, without WriteTool.
			if err := r.Register(agenttools.Tool{Name: "raw_write", Kind: registry.ToolWrite, Input: []byte(subjectSchema), Handle: writeHandler(tc.res)}); err != nil {
				t.Fatalf("Register: %v", err)
			}
			for _, p := range []agenttools.Principal{agent("r1"), user()} {
				_, err := r.Call(ctx, p, "raw_write", json.RawMessage(`{"subject":"s1"}`))
				if tc.ok && err != nil {
					t.Fatalf("%s Call: %v", p.Kind, err)
				}
				if !tc.ok && !errors.Is(err, agenttools.ErrBadTool) {
					t.Fatalf("%s Call err = %v, want ErrBadTool", p.Kind, err)
				}
			}
		})
	}
}
