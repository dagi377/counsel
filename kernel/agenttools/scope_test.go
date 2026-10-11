package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// toolGapKinds reads the gate kinds spec/ai declares as tool gaps: kinds features open
// through draft_document + submit_for_approval.
func toolGapKinds(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read features: %v", err)
	}
	var f struct {
		ToolGaps struct {
			Kinds []string `json:"kinds"`
		} `json:"tool_gaps"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse features: %v", err)
	}
	if len(f.ToolGaps.Kinds) != 38 {
		t.Fatalf("tool_gap kinds = %d, want 38", len(f.ToolGaps.Kinds))
	}
	return f.ToolGaps.Kinds
}

// opener records every gate it opens.
type opener struct{ opened []string }

func (o *opener) open(_ context.Context, kind string, s registry.Subject, _ json.RawMessage, _ agenttools.Principal) (gate.GateID, error) {
	o.opened = append(o.opened, kind+"/"+s.ID)
	return gate.GateID(fmt.Sprintf("gate-%d", len(o.opened))), nil
}

const draftRef = "6f1c1a52-0000-4000-8000-000000000001"

// scopedRegistry registers draft_document (returns a draft) and submit_for_approval.
func scopedRegistry(t *testing.T, o *opener, runID string) *agenttools.Registry {
	t.Helper()
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(context.Background(), runID, []string{"draft", "submit"}); err != nil {
		t.Fatalf("ReportPlan: %v", err)
	}
	r := agenttools.NewRegistry(plans)
	draft, err := agenttools.WriteTool(agenttools.Tool{
		Name: "draft_document", Kind: registry.ToolWrite, Capability: "docs.draft", Input: []byte(`{"type":"object"}`),
		Handle: writeHandler(agenttools.ToolResult{Output: out("draft", draftRef)}),
	})
	if err != nil {
		t.Fatalf("draft tool: %v", err)
	}
	submit, err := agenttools.SubmitForApprovalTool(o.open)
	if err != nil {
		t.Fatalf("submit tool: %v", err)
	}
	og, err := agenttools.OpenGateTool(o.open, "")
	if err != nil {
		t.Fatalf("open_gate tool: %v", err)
	}
	for _, tool := range []agenttools.Tool{draft, submit, og} {
		if err := r.Register(tool); err != nil {
			t.Fatalf("Register %s: %v", tool.Name, err)
		}
	}
	return r
}

func submitIn(kind, subject string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"subject_type": "document", "subject_id": subject, "gate_kind": kind, "note": "please review"})
	return b
}

// rule: ARC-I-035
// rule: AI-R-003
func TestARC_I_035_SubmitForApprovalGateKindOutsideScope(t *testing.T) {
	kinds := toolGapKinds(t)
	// The feature declares one kind; every other tool-gap kind is outside its scope.
	declared := kinds[0]
	type tc struct {
		kind string
		want error
	}
	cases := []tc{{kind: declared}}
	for _, k := range kinds[1:] {
		cases = append(cases, tc{kind: k, want: agenttools.ErrGateKindNotAllowed})
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			o := &opener{}
			r := scopedRegistry(t, o, "run-1")
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope("run-1", []string{declared}))
			p := agent("run-1", "docs.draft", agenttools.CapAgentAsk)
			if _, err := r.Call(ctx, p, "draft_document", json.RawMessage(`{}`)); err != nil {
				t.Fatalf("draft: %v", err)
			}
			res, err := r.Call(ctx, p, agenttools.ToolSubmitForApproval, submitIn(c.kind, draftRef))
			if !errors.Is(err, c.want) {
				t.Fatalf("submit err = %v, want %v", err, c.want)
			}
			if c.want != nil {
				if len(o.opened) != 0 {
					t.Fatalf("opened %v, want no gate", o.opened)
				}
				return
			}
			if res.GateID == "" || len(o.opened) != 1 || o.opened[0] != declared+"/"+draftRef {
				t.Fatalf("res = %+v opened %v", res, o.opened)
			}
		})
	}
}

// rule: ARC-I-035
func TestARC_I_035_OpenGateKindOutsideScope(t *testing.T) {
	cases := []struct {
		name  string
		scope *agenttools.Scope
		p     agenttools.Principal
		kind  string
		want  error
	}{
		{name: "declared kind", scope: agenttools.NewScope("run-1", []string{"records.publish"}), p: agent("run-1"), kind: "records.publish"},
		{name: "undeclared kind", scope: agenttools.NewScope("run-1", []string{"records.publish"}), p: agent("run-1"), kind: "records.delete", want: agenttools.ErrGateKindNotAllowed},
		{name: "agent without scope fails closed", p: agent("run-1"), kind: "records.publish", want: agenttools.ErrGateKindNotAllowed},
		{name: "scope of another run", scope: agenttools.NewScope("run-2", []string{"records.publish"}), p: agent("run-1"), kind: "records.publish", want: agenttools.ErrGateKindNotAllowed},
		{name: "user manual path has no run scope", p: user(), kind: "records.delete"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &opener{}
			r := scopedRegistry(t, o, "run-1")
			ctx := context.Background()
			if c.scope != nil {
				ctx = agenttools.WithScope(ctx, c.scope)
			}
			in := fmt.Sprintf(`{"kind":%q,"subject":{"kind":"record","id":"x1"},"payload":{}}`, c.kind)
			_, err := r.Call(ctx, c.p, agenttools.ToolOpenGate, json.RawMessage(in))
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if wantOpened := c.want == nil; wantOpened != (len(o.opened) == 1) {
				t.Fatalf("opened = %v", o.opened)
			}
		})
	}
}

// rule: AI-R-003
func TestAI_R_003_SubmitForApprovalOnlyDraftsOfThisRun(t *testing.T) {
	const kind = "records.review"
	cases := []struct {
		name    string
		draft   bool // run draft_document first
		subject string
		want    error
	}{
		{name: "draft of this run", draft: true, subject: draftRef},
		{name: "no draft in run", subject: draftRef, want: agenttools.ErrNotFound},
		{name: "other subject", draft: true, subject: "6f1c1a52-0000-4000-8000-000000000099", want: agenttools.ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := &opener{}
			r := scopedRegistry(t, o, "run-1")
			ctx := agenttools.WithScope(context.Background(), agenttools.NewScope("run-1", []string{kind}))
			p := agent("run-1", "docs.draft", agenttools.CapAgentAsk)
			if c.draft {
				if _, err := r.Call(ctx, p, "draft_document", json.RawMessage(`{}`)); err != nil {
					t.Fatalf("draft: %v", err)
				}
			}
			_, err := r.Call(ctx, p, agenttools.ToolSubmitForApproval, submitIn(kind, c.subject))
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if wantOpened := c.want == nil; wantOpened != (len(o.opened) == 1) {
				t.Fatalf("opened = %v", o.opened)
			}
		})
	}
}

func TestScopeOf(t *testing.T) {
	s := agenttools.NewScope("run-1", []string{"a.b"})
	cases := []struct {
		name string
		ctx  context.Context
		ok   bool
	}{
		{name: "none", ctx: context.Background()},
		{name: "stored", ctx: agenttools.WithScope(context.Background(), s), ok: true},
		{name: "nil scope", ctx: agenttools.WithScope(context.Background(), nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := agenttools.ScopeOf(c.ctx)
			if ok != c.ok || (ok && got != s) {
				t.Fatalf("ScopeOf = %v, %v", got, ok)
			}
		})
	}
	s.AddDraft("d1")
	s.AddDraft("d1")
	if len(s.Drafts) != 1 || !s.HasDraft("d1") || s.HasDraft("d2") || !s.AllowsGateKind("a.b") || s.AllowsGateKind("c.d") {
		t.Fatalf("scope = %+v", s)
	}
	if _, err := agenttools.SubmitForApprovalTool(nil); !errors.Is(err, agenttools.ErrBadTool) {
		t.Fatalf("SubmitForApprovalTool(nil) err = %v", err)
	}
}
