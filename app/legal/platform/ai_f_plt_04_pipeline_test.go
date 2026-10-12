package platform_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Synthetic call transcript passages; the second carries an injection.
var pltCallTranscript = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-call-synthetic", Page: 1, FromLine: 1, ToLine: 12}, Text: "Call 2026-10-08 lasted 42 minutes. Lawyer and client reviewed the mediation brief. The client will send the signed retainer by Friday."},
	{Source: agenttools.Citation{VersionID: "ver-call-synthetic", Page: 1, FromLine: 13, ToLine: 14}, Text: pltInjected},
}

// pltCallBrief is page 1 of the matter brief the run reads with read_document.
var pltCallBrief = []string{"Mediation brief for the synthetic estate file.", "The brief sets out the client's position on the estate accounts."}

// pltCallSink records what the pipeline files.
type pltCallSink struct {
	calls int
	items []platform.CallItem
}

func (s *pltCallSink) FileCallSummary(_ context.Context, featureID string, items []platform.CallItem) error {
	if featureID != platform.FeatureCallSummarizer {
		return errors.New("wrong feature id")
	}
	s.calls++
	s.items = append(s.items, items...)
	return nil
}

// pltCallWorld is one seeded run of the call summarizer over the app's real read handlers.
type pltCallWorld struct {
	fd       *agenttest.FeatureDriver
	matterID string
	walledID string
	docID    string
	version  string
}

func pltCallSeed(t *testing.T) pltCallWorld {
	t.Helper()
	fd := agenttest.FromFeature(t, platform.CallSummarizerDef())
	// rule: ARC-I-035
	for _, name := range []string{agent.ToolGetMatter, agent.ToolSearchDocuments, agent.ToolReadDocument} {
		if !slices.Contains(fd.Real, name) {
			t.Fatalf("%s runs as a stand-in: real %v", name, fd.Real)
		}
	}
	m := fd.Seed.Matter("2026-0811", "Synth estate mediation")
	walled := fd.Seed.Matter("2026-0899", "Synth walled file")
	fd.Seed.Wall(string(walled.ID))
	docID, v := fd.Seed.Document(string(m.ID), pltCallBrief)
	return pltCallWorld{fd: fd, matterID: string(m.ID), walledID: string(walled.ID), docID: docID, version: string(v)}
}

// pltCallItems is the scripted final output of items.
func pltCallItems(items ...map[string]any) map[string]any {
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{"json": map[string]any{"items": items}}
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
func TestARC_I_038_CallSummarizerPipeline(t *testing.T) {
	call := pltCite(agenttools.Citation{VersionID: "ver-call-synthetic", Page: 1, FromLine: 2, ToLine: 6})
	summary := map[string]any{"kind": platform.CallSummary, "text": "Lawyer and client reviewed the mediation brief.", "citation": call}
	task := map[string]any{"kind": platform.CallTask, "text": "Client sends the signed retainer by Friday.", "citation": call}
	entry := map[string]any{"kind": platform.CallTimeEntry, "text": "Call with client re mediation brief", "work_date": "2026-10-08", "minutes": 42, "citation": call}
	timeEntry := func(date string, minutes int) map[string]any {
		return map[string]any{"kind": platform.CallTimeEntry, "text": "Call", "work_date": date, "minutes": minutes, "citation": call}
	}
	g, g9 := platform.GateSummaryAccuracy, agent.GateTimeAccept
	cases := []struct {
		name       string
		walled     bool // get_matter asks for the walled matter instead
		final      func(w pltCallWorld) map[string]any
		opts       []aifake.Option
		wantErr    error
		wantKinds  []string
		wantRefuse bool // get_matter is refused
	}{
		{name: "cited summary, task and time entry", final: func(w pltCallWorld) map[string]any {
			// The brief paragraph cites the page read_document returned in this run.
			brief := map[string]any{"kind": platform.CallSummary, "text": "The brief sets out the client's position.", "citation": pltCite(agenttools.Citation{VersionID: w.version, Page: 1, FromLine: 2, ToLine: 2})}
			return pltCallItems(summary, brief, task, entry)
		}, wantKinds: []string{g, g, g, g9}},
		// rule: ARC-I-038
		{name: "injection stays quoted and adds no item", final: func(pltCallWorld) map[string]any { return pltCallItems(summary, entry) }, wantKinds: []string{g, g9}},
		{name: "nothing to file", final: func(pltCallWorld) map[string]any { return pltCallItems() }},
		// rule: AI-R-003
		{name: "walled matter read refused", walled: true, final: func(pltCallWorld) map[string]any { return pltCallItems(summary) }, wantKinds: []string{g}, wantRefuse: true},
		{name: "invalid model json", final: func(pltCallWorld) map[string]any { return map[string]any{"text": "The call went well."} }, wantErr: run.ErrBadOutput},
		{name: "fails schema", final: func(pltCallWorld) map[string]any {
			return pltCallItems(map[string]any{"kind": platform.CallSummary, "text": "", "citation": call})
		}, wantErr: run.ErrBadOutput},
		{name: "item without citation", final: func(pltCallWorld) map[string]any {
			return pltCallItems(summary, map[string]any{"kind": platform.CallTask, "text": "x"})
		}, wantErr: run.ErrBadOutput},
		{name: "cites an unquoted source", final: func(pltCallWorld) map[string]any {
			return pltCallItems(map[string]any{"kind": platform.CallSummary, "text": "x", "citation": pltCite(agenttools.Citation{VersionID: "ver-other", Page: 1, FromLine: 2, ToLine: 3})})
		}, wantErr: run.ErrUncitedAnswer},
		{name: "cites outside the source lines", final: func(pltCallWorld) map[string]any {
			return pltCallItems(map[string]any{"kind": platform.CallSummary, "text": "x", "citation": pltCite(agenttools.Citation{VersionID: "ver-call-synthetic", Page: 1, FromLine: 10, ToLine: 13})})
		}, wantErr: run.ErrUncitedAnswer},
		// rule: ARC-U-027
		{name: "work date no source stated", final: func(pltCallWorld) map[string]any { return pltCallItems(summary, timeEntry("2026-10-09", 42)) }, wantErr: agenttools.ErrUnsourcedValue},
		{name: "minutes no source stated", final: func(pltCallWorld) map[string]any { return pltCallItems(summary, timeEntry("2026-10-08", 45)) }, wantErr: agenttools.ErrUnsourcedValue},
		{name: "time entry without minutes", final: func(pltCallWorld) map[string]any {
			return pltCallItems(map[string]any{"kind": platform.CallTimeEntry, "text": "Call", "work_date": "2026-10-08", "citation": call})
		}, wantErr: platform.ErrBadCallItem},
		{name: "time entry date not on the calendar", final: func(pltCallWorld) map[string]any { return pltCallItems(timeEntry("2026-02-30", 42)) }, wantErr: platform.ErrBadCallItem},
		{name: "summary with time fields", final: func(pltCallWorld) map[string]any {
			return pltCallItems(map[string]any{"kind": platform.CallSummary, "text": "x", "minutes": 42, "citation": call})
		}, wantErr: platform.ErrBadCallItem},
		{name: "rate limited queues a retry", final: func(pltCallWorld) map[string]any { return pltCallItems(summary) }, opts: []aifake.Option{aifake.WithFault(platform.FeatureCallSummarizer, aifake.FaultRateLimit)}, wantErr: platform.ErrCallRetryQueued},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := pltCaptureLogs(t)
			w := pltCallSeed(t)
			matterID := w.matterID
			if tc.walled {
				matterID = w.walledID
			}
			calls := []pltCall{
				{agent.ToolGetMatter, map[string]any{"matter_id": matterID}},
				{agent.ToolReadDocument, map[string]any{"doc_id": w.docID, "version_id": w.version}},
			}
			model := &pltRecorder{ModelClient: pltScript(t, platform.FeatureCallSummarizer, calls, tc.final(w), tc.opts...)}
			sink := &pltCallSink{}
			p := platform.CallSummarizerPipeline{Runner: run.New(model, w.fd.Registry), Sink: sink}
			principal := run.Principal{Kind: registry.PrincipalAgent, RunID: "run-plt-04-" + strings.ReplaceAll(tc.name, " ", "-"), ActingFor: agenttest.FeatureActingFor}

			items, err := p.Run(context.Background(), principal, pltCallTranscript)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			// rule: AI-R-005
			// Only the sink receives items: the run itself writes no draft and opens no gate.
			if len(w.fd.Seed.Store.Drafts) != 0 {
				t.Fatalf("run wrote %d drafts", len(w.fd.Seed.Store.Drafts))
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
				if err := agenttools.ValidCitation(it.Citation); err != nil {
					t.Fatalf("item %d citation: %v", i, err)
				}
				if strings.Contains(it.Text, "ignore previous") {
					t.Fatalf("item %d carries the injected text", i)
				}
			}
			// The model made exactly the scripted calls, as one tool-use run offering the 9
			// feature tools, acting for the preparing user.
			last := model.reqs[len(model.reqs)-1]
			if len(model.reqs) != len(calls)+1 || last.Kind != ai.KindToolUse || len(last.Tools) != 9 {
				t.Fatalf("model requests = %d kind %s tools %d; want %d tool-use calls offering 9 tools", len(model.reqs), last.Kind, len(last.Tools), len(calls)+1)
			}
			if !strings.HasPrefix(last.System, feature.Preamble(platform.FeatureCallSummarizer)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			if md := last.Metadata; md["acting_for"] != agenttest.FeatureActingFor {
				t.Fatalf("run does not act for the preparing user")
			}
			var uses []string
			refused := false
			for _, m := range last.Messages {
				for _, b := range m.Content {
					if b.Type == "tool_use" {
						uses = append(uses, b.ToolName)
					}
					if b.Type == "tool_result" && b.ToolName == agent.ToolGetMatter {
						refused = b.IsError
						if strings.Contains(b.Text, "Synth walled file") {
							t.Fatalf("walled matter reached the model")
						}
					}
				}
			}
			// rule: ARC-I-038
			if want := []string{agent.ToolGetMatter, agent.ToolReadDocument}; !slices.Equal(uses, want) {
				t.Fatalf("tool calls = %v, want %v (no extra tool call)", uses, want)
			}
			if refused != tc.wantRefuse {
				t.Fatalf("get_matter refused = %v, want %v", refused, tc.wantRefuse)
			}
			for _, s := range pltCallTranscript {
				pltQuotedOnly(t, model.reqs, s.Text)
			}
			// rule: AI-R-005
			for _, s := range []string{pltCallTranscript[0].Text, pltInjected, "mediation brief", "retainer"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries transcript or output text")
				}
			}
		})
	}
}

func TestCallSummarizerPipelineRefusesBadInput(t *testing.T) {
	tests := []struct {
		name       string
		p          run.Principal
		transcript []agenttools.Quoted
		wantErr    error
	}{
		{"user principal", run.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, pltCallTranscript, run.ErrNotAgent},
		{"passage without citation", run.Principal{Kind: registry.PrincipalAgent, RunID: "run-plt-04-bad", ActingFor: agenttest.FeatureActingFor}, []agenttools.Quoted{{Text: "transcript"}}, agenttools.ErrCitation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := pltCallSeed(t)
			model := &pltRecorder{ModelClient: pltScript(t, platform.FeatureCallSummarizer, nil, pltCallItems())}
			sink := &pltCallSink{}
			p := platform.CallSummarizerPipeline{Runner: run.New(model, w.fd.Registry), Sink: sink}
			if _, err := p.Run(context.Background(), tt.p, tt.transcript); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(model.reqs) != 0 || sink.calls != 0 {
				t.Fatalf("model called %d times, wrote %d", len(model.reqs), sink.calls)
			}
		})
	}
}

// rule: AI-R-003
// rule: ARC-I-035
func TestAI_R_003_CallSummarizerDef(t *testing.T) {
	d := platform.CallSummarizerDef()
	pltCheckDefMatchesSpec(t, d)
	pltCheckPrompt(t, d, "Call summarizer", platform.GateSummaryAccuracy, agent.GateTimeAccept, "quoted")
	if len(d.OutputSchema) == 0 {
		t.Fatal("tool-use feature without output schema")
	}
}

// rule: AI-R-003
// rule: ARC-I-039
func TestFPLT04_AgentNeverApproves(t *testing.T) {
	pltCheckNoApproval(t, platform.CallSummarizerDef())
}
