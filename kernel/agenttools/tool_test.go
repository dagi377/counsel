package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const subjectSchema = `{"type":"object","required":["subject"],"properties":{"subject":{"type":"string","minLength":1}},"additionalProperties":false}`

func okHandler(_ context.Context, _ agenttools.Principal, _ json.RawMessage) (agenttools.ToolResult, error) {
	return agenttools.ToolResult{Output: json.RawMessage(`{"ok":true}`)}, nil
}

func tool(name string, kind agenttools.ToolKind, capability string) agenttools.Tool {
	return agenttools.Tool{Name: name, Kind: kind, Input: []byte(subjectSchema), Capability: capability, Handle: okHandler}
}

func agent(run string, grant ...string) agenttools.Principal {
	return agenttools.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: "u1", Grant: grant}
}

func user() agenttools.Principal {
	return agenttools.Principal{Kind: registry.PrincipalUser, UserID: "u1"}
}

// rule: ARC-I-035
func TestARC_I_035_RegisterRejectsBadTools(t *testing.T) {
	cases := []struct {
		name string
		tool agenttools.Tool
		ok   bool
	}{
		{"read tool", tool("get_subject", registry.ToolRead, ""), true},
		{"report tool", tool("report_note", registry.ToolReport, ""), true},
		{"write tool", tool("draft_record", registry.ToolWrite, "records.draft"), true},
		{"unknown kind", tool("draft_x", "effect", ""), false},
		{"send prefix", tool("send_email", registry.ToolWrite, ""), false},
		{"approve prefix", tool("approve_bill", registry.ToolWrite, ""), false},
		{"authorize prefix", tool("authorize_payment", registry.ToolWrite, ""), false},
		{"execute prefix", tool("execute_transfer", registry.ToolWrite, ""), false},
		{"empty name", tool("", registry.ToolRead, ""), false},
		{"no handler", agenttools.Tool{Name: "h", Kind: registry.ToolRead, Input: []byte(subjectSchema)}, false},
		{"no schema", agenttools.Tool{Name: "s", Kind: registry.ToolRead, Handle: okHandler}, false},
		{"broken schema", agenttools.Tool{Name: "b", Kind: registry.ToolRead, Input: []byte(`{"type":`), Handle: okHandler}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := agenttools.NewRegistry(agenttoolsfake.New())
			err := r.Register(tc.tool)
			if tc.ok && err != nil {
				t.Fatalf("Register: %v", err)
			}
			if !tc.ok && !errors.Is(err, agenttools.ErrBadTool) {
				t.Fatalf("Register err = %v, want ErrBadTool", err)
			}
		})
	}
	r := agenttools.NewRegistry(agenttoolsfake.New())
	if err := r.Register(tool("dup", registry.ToolRead, "")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(tool("dup", registry.ToolRead, "")); !errors.Is(err, agenttools.ErrBadTool) {
		t.Fatalf("duplicate err = %v, want ErrBadTool", err)
	}
}

// rule: ARC-I-036
func TestARC_I_036_AgentNeedsGrantedCapability(t *testing.T) {
	cases := []struct {
		name    string
		p       agenttools.Principal
		wantErr error
	}{
		{"agent without capability", agent("r1"), agenttools.ErrMissingCapability},
		{"agent with other capability", agent("r1", "records.read"), agenttools.ErrMissingCapability},
		{"agent with capability", agent("r1", "records.lookup"), nil},
		{"user is checked by identity, not grants", user(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := agenttools.NewRegistry(agenttoolsfake.New())
			if err := r.Register(tool("get_subject", registry.ToolRead, "records.lookup")); err != nil {
				t.Fatal(err)
			}
			_, err := r.Call(context.Background(), tc.p, "get_subject", json.RawMessage(`{"subject":"s1"}`))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Call err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestCallValidatesInputSchema(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		in      string
		wantErr error
	}{
		{"valid", "get_subject", `{"subject":"s1"}`, nil},
		{"missing field", "get_subject", `{}`, agenttools.ErrInvalidInput},
		{"wrong type", "get_subject", `{"subject":7}`, agenttools.ErrInvalidInput},
		{"extra field", "get_subject", `{"subject":"s1","x":1}`, agenttools.ErrInvalidInput},
		{"not json", "get_subject", `{`, agenttools.ErrInvalidInput},
		{"unknown tool", "nope", `{"subject":"s1"}`, agenttools.ErrUnknownTool},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := agenttools.NewRegistry(agenttoolsfake.New())
			if err := r.Register(tool("get_subject", registry.ToolRead, "")); err != nil {
				t.Fatal(err)
			}
			_, err := r.Call(context.Background(), agent("r1"), tc.tool, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Call err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// rule: ARC-I-039
func TestARC_I_039_WriteBeforePlanIsPlanRequired(t *testing.T) {
	cases := []struct {
		name    string
		kind    agenttools.ToolKind
		plan    bool
		wantErr error
	}{
		{"write without plan", registry.ToolWrite, false, agenttools.ErrPlanRequired},
		{"write after report_plan", registry.ToolWrite, true, nil},
		{"read without plan", registry.ToolRead, false, nil},
		{"report without plan", registry.ToolReport, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			plans := agenttoolsfake.New()
			r := agenttools.NewRegistry(plans)
			if err := r.Register(tool("draft_record", tc.kind, "")); err != nil {
				t.Fatal(err)
			}
			if tc.plan {
				if _, err := plans.ReportPlan(ctx, "r1", []string{"draft-record"}); err != nil {
					t.Fatal(err)
				}
			}
			_, err := r.Call(ctx, agent("r1"), "draft_record", json.RawMessage(`{"subject":"s1"}`))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Call err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// rule: ARC-U-029
func TestARC_U_029_ManualPathRunsWithoutAgentRun(t *testing.T) {
	cases := []struct {
		name    string
		p       agenttools.Principal
		wantErr error
	}{
		{"user calls write tool with no run or plan", user(), nil},
		{"agent without plan still gated", agent("r1"), agenttools.ErrPlanRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := agenttools.NewRegistry(agenttoolsfake.New())
			if err := r.Register(tool("draft_record", registry.ToolWrite, "")); err != nil {
				t.Fatal(err)
			}
			res, err := r.Call(context.Background(), tc.p, "draft_record", json.RawMessage(`{"subject":"s1"}`))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Call err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && string(res.Output) != `{"ok":true}` {
				t.Fatalf("Output = %s", res.Output)
			}
		})
	}
}
