package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/registry"
)

// plt06File is a synthetic concluded matter's file in three passages; the second carries an
// injection. Amounts are stated in cents, as the ledger exports them.
var plt06File = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-close-1", Page: 1, FromLine: 1, ToLine: 4}, Text: "File 2026-0306 reached the concluded stage on 2026-10-02. Unbilled work since the last account: 180000 cents."},
	{Source: agenttools.Citation{VersionID: "ver-close-1", Page: 1, FromLine: 5, ToLine: 6}, Text: pltkInjected},
	{Source: agenttools.Citation{VersionID: "ver-close-1", Page: 1, FromLine: 7, ToLine: 9}, Text: "Trust ledger statement: 250000 cents held for the client; no cheque outstanding."},
}

var (
	plt06WorkCite  = pltkCite(agenttools.Citation{VersionID: "ver-close-1", Page: 1, FromLine: 1, ToLine: 4})
	plt06TrustCite = pltkCite(agenttools.Citation{VersionID: "ver-close-1", Page: 1, FromLine: 7, ToLine: 9})
	plt06Bill      = `{"kind": "final_bill", "text": "Final account for the unbilled work since the last account.", "amount_cents": 180000, "citation": ` + plt06WorkCite + `}`
	plt06Refund    = `{"kind": "trust_refund", "text": "Refund the trust balance to the client by cheque.", "amount_cents": 250000, "citation": ` + plt06TrustCite + `}`
	plt06Checklist = `{"kind": "closing_checklist", "text": "Closing checklist and closing letter: report the result, return originals, confirm the file closes.", "citation": ` + plt06WorkCite + `}`
)

func plt06Reply(items ...string) string {
	return `{"json": {"items": [` + strings.Join(items, ", ") + `]}}`
}

// plt06Run is the agent principal the pipeline runs as, acting for the preparing lawyer.
var plt06Run = run.Principal{Kind: registry.PrincipalAgent, RunID: "run-fplt06", ActingFor: "user-synthetic-closing-lawyer"}

// plt06Sink records what the pipeline files.
type plt06Sink struct {
	calls int
	items []platform.ClosingItem
}

func (s *plt06Sink) FileClosing(_ context.Context, featureID string, items []platform.ClosingItem) error {
	if featureID != platform.FeatureClosingStarter {
		return errors.New("wrong feature id")
	}
	s.calls++
	s.items = append(s.items, items...)
	return nil
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFPLT06_DefMatchesSpec(t *testing.T) {
	d := platform.ClosingStarterDef()
	pltfCheckDefMatchesSpec(t, d)
	if len(d.OutputSchema) == 0 {
		t.Fatal("tool-use pipeline without output schema")
	}
}

// rule: AI-R-005
func TestFPLT06_Prompt(t *testing.T) {
	pltfCheckPrompt(t, platform.ClosingStarterDef(), "Closing starter", "Drafts for G-B2, G11 and G13",
		bill.GateBillApproval, agent.GateTrustWithdrawal, matter.GateClosing, "report_plan", "overdraft", "integer cents")
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
func TestARC_I_038_ClosingStarterPipeline(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		opts      []aifake.Option
		wantErr   error
		wantKinds []string
	}{
		{"cited drafts for G-B2, G11 and G13", plt06Reply(plt06Bill, plt06Refund, plt06Checklist), nil, nil,
			[]string{bill.GateBillApproval, agent.GateTrustWithdrawal, matter.GateClosing}},
		{"injection stays quoted and adds no item", plt06Reply(plt06Checklist), nil, nil, []string{matter.GateClosing}},
		{"nothing to close", plt06Reply(), nil, nil, nil},
		{"invalid model json", `{"text": "Close the file and refund the client."}`, nil, run.ErrBadOutput, nil},
		{"fails schema", plt06Reply(`{"kind": "approve_bill", "text": "x", "citation": ` + plt06WorkCite + `}`), nil, run.ErrBadOutput, nil},
		{"item without citation", plt06Reply(plt06Checklist, `{"kind": "final_bill", "text": "x"}`), nil, run.ErrBadOutput, nil},
		{"cites an unquoted source", plt06Reply(`{"kind": "closing_checklist", "text": "x", "citation": {"version_id": "ver-other", "page": 1, "from_line": 1, "to_line": 2}}`), nil, run.ErrUncitedAnswer, nil},
		{"cites the injected lines outside a source", plt06Reply(`{"kind": "closing_checklist", "text": "x", "citation": {"version_id": "ver-close-1", "page": 1, "from_line": 4, "to_line": 7}}`), nil, run.ErrUncitedAnswer, nil},
		// rule: ARC-U-027
		{"refund amount no source states", plt06Reply(`{"kind": "trust_refund", "text": "Refund", "amount_cents": 260000, "citation": ` + plt06TrustCite + `}`), nil, agenttools.ErrUnsourcedValue, nil},
		{"refund without an amount", plt06Reply(`{"kind": "trust_refund", "text": "Refund", "citation": ` + plt06TrustCite + `}`), nil, platform.ErrBadClosingItem, nil},
		{"two refunds", plt06Reply(plt06Refund, plt06Refund), nil, platform.ErrBadClosingItem, nil},
		{"checklist with an amount", plt06Reply(`{"kind": "closing_checklist", "text": "x", "amount_cents": 180000, "citation": ` + plt06WorkCite + `}`), nil, platform.ErrBadClosingItem, nil},
		{"rate limited queues a retry", plt06Reply(plt06Checklist), []aifake.Option{aifake.WithFault(platform.FeatureClosingStarter, aifake.FaultRateLimit)}, platform.ErrClosingRetryQueued, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := pltkCaptureLogs(t)
			model := &pltkRecorder{ModelClient: pltkScripted(t, platform.FeatureClosingStarter, tc.reply, tc.opts...)}
			sink := &plt06Sink{}
			p := platform.ClosingStarterPipeline{Runner: run.New(model, agenttoolsfake.New()), Sink: sink}

			items, err := p.Run(context.Background(), plt06Run, plt06File)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// Nothing is written when the output does not check out.
				if items != nil || sink.calls != 0 {
					t.Fatalf("wrote %d items in %d calls on error", len(sink.items), sink.calls)
				}
				return
			}
			if len(items) != len(tc.wantKinds) || len(sink.items) != len(tc.wantKinds) || sink.calls != min(len(tc.wantKinds), 1) {
				t.Fatalf("items = %d, written %d in %d calls, want %d", len(items), len(sink.items), sink.calls, len(tc.wantKinds))
			}
			for i, it := range sink.items {
				if it.GateKind() != tc.wantKinds[i] {
					t.Fatalf("item %d gate kind = %s, want %s", i, it.GateKind(), tc.wantKinds[i])
				}
				// rule: ARC-I-038
				// Every item cites a passage it was given, never the injected one.
				if err := agenttools.ValidCitation(it.Citation); err != nil || it.Citation.VersionID != "ver-close-1" ||
					(it.Citation.FromLine <= plt06File[1].Source.ToLine && it.Citation.ToLine >= plt06File[1].Source.FromLine) {
					t.Fatalf("item %d does not cite a quoted passage: %+v %v", i, it.Citation, err)
				}
				if strings.Contains(it.Text, "ignore previous") {
					t.Fatalf("item %d carries the injected text", i)
				}
			}
			if len(model.reqs) != 1 || model.reqs[0].Kind != ai.KindToolUse || len(model.reqs[0].Tools) != len(platform.ClosingStarterDef().Tools) {
				t.Fatalf("model requests = %d, want one tool-use call offering the feature's tools", len(model.reqs))
			}
			if !strings.HasPrefix(model.reqs[0].System, feature.Preamble(platform.FeatureClosingStarter)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			if md := model.reqs[0].Metadata; md["acting_for"] != plt06Run.ActingFor {
				t.Fatalf("run does not act for the preparing user")
			}
			for _, s := range plt06File {
				pltkQuotedOnly(t, model.reqs, s.Text)
			}
			// rule: AI-R-005
			for _, s := range []string{plt06File[0].Text, pltkInjected, "180000", "Refund the trust balance"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries file or output text")
				}
			}
		})
	}
}

func TestClosingStarterPipelineRefusesBadInput(t *testing.T) {
	tests := []struct {
		name    string
		p       run.Principal
		file    []agenttools.Quoted
		wantErr error
	}{
		{"user principal", run.Principal{Kind: registry.PrincipalUser, UserID: "user-synthetic-closing-lawyer"}, plt06File, run.ErrNotAgent},
		{"passage without citation", plt06Run, []agenttools.Quoted{{Text: "file"}}, agenttools.ErrCitation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &pltkRecorder{ModelClient: pltkScripted(t, platform.FeatureClosingStarter, plt06Reply(plt06Checklist))}
			sink := &plt06Sink{}
			p := platform.ClosingStarterPipeline{Runner: run.New(model, agenttoolsfake.New()), Sink: sink}
			if _, err := p.Run(context.Background(), tt.p, tt.file); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(model.reqs) != 0 || sink.calls != 0 {
				t.Fatalf("model called %d times, wrote %d", len(model.reqs), sink.calls)
			}
		})
	}
}
