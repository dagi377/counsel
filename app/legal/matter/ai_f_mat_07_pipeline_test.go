package matter_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Synthetic call transcript passages; the second carries an injection.
var matCallTranscript = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-call-1", Page: 1, FromLine: 1, ToLine: 12}, Text: "Call 2026-10-08, 10:00 to 10:42. Lawyer and client reviewed the mediation brief and agreed to serve it Friday."},
	{Source: agenttools.Citation{VersionID: "ver-call-1", Page: 1, FromLine: 13, ToLine: 14}, Text: matInjected},
}

// matCallSink records what the pipeline files.
type matCallSink struct {
	calls int
	items []matter.CallSummaryItem
}

func (s *matCallSink) FileCallSummary(_ context.Context, featureID string, items []matter.CallSummaryItem) error {
	if featureID != matter.FeatureCallSummary {
		return errors.New("wrong feature id")
	}
	s.calls++
	s.items = append(s.items, items...)
	return nil
}

var matCallRun = run.Principal{Kind: registry.PrincipalAgent, RunID: "run-mat-07", ActingFor: "user-lawyer-1"}

var (
	matCallCite  = matCite(agenttools.Citation{VersionID: "ver-call-1", Page: 1, FromLine: 2, ToLine: 6})
	matCallNote  = `{"kind": "note", "text": "Reviewed the mediation brief; it is served Friday.", "citation": ` + matCallCite + `}`
	matCallEntry = `{"kind": "time_entry", "text": "Call with client re mediation brief", "work_date": "2026-10-08", "minutes": 42, "citation": ` + matCallCite + `}`
)

func matCallReply(items ...string) string {
	return `{"json": {"items": [` + strings.Join(items, ", ") + `]}}`
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
func TestARC_I_038_CallSummaryPipeline(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		opts      []aifake.Option
		wantErr   error
		wantKinds []string
	}{
		{"cited note and time entry", matCallReply(matCallNote, matCallEntry), nil, nil, []string{matter.GateAgentNote, matter.GateTimeAccept}},
		{"injection stays quoted and adds no item", matCallReply(matCallNote), nil, nil, []string{matter.GateAgentNote}},
		{"nothing to file", matCallReply(), nil, nil, nil},
		{"invalid model json", `{"text": "The call went well."}`, nil, run.ErrBadOutput, nil},
		{"fails schema", matCallReply(`{"kind": "note", "text": "", "citation": ` + matCallCite + `}`), nil, run.ErrBadOutput, nil},
		{"item without citation", matCallReply(matCallNote, `{"kind": "note", "text": "x"}`), nil, run.ErrBadOutput, nil},
		{"cites an unquoted source", matCallReply(`{"kind": "note", "text": "x", "citation": {"version_id": "ver-other", "page": 1, "from_line": 2, "to_line": 3}}`), nil, run.ErrUncitedAnswer, nil},
		{"cites outside the source lines", matCallReply(`{"kind": "note", "text": "x", "citation": {"version_id": "ver-call-1", "page": 1, "from_line": 10, "to_line": 13}}`), nil, run.ErrUncitedAnswer, nil},
		{"time entry without minutes", matCallReply(`{"kind": "time_entry", "text": "Call", "work_date": "2026-10-08", "citation": ` + matCallCite + `}`), nil, matter.ErrBadItem, nil},
		{"time entry date not on the calendar", matCallReply(`{"kind": "time_entry", "text": "Call", "work_date": "2026-02-30", "minutes": 5, "citation": ` + matCallCite + `}`), nil, matter.ErrBadItem, nil},
		{"rate limited queues a retry", matCallReply(matCallNote), []aifake.Option{aifake.WithFault(matter.FeatureCallSummary, aifake.FaultRateLimit)}, matter.ErrRetryQueued, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := matCaptureLogs(t)
			fake := matScripted(t, matter.FeatureCallSummary, tc.reply, tc.opts...)
			model := &matRecorder{ModelClient: fake}
			sink := &matCallSink{}
			p := matter.CallSummaryPipeline{Runner: run.New(model, agenttoolsfake.New()), Sink: sink}

			items, err := p.Run(context.Background(), matCallRun, matCallTranscript)
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
				if err := agenttools.ValidCitation(it.Citation); err != nil || it.Citation.ToLine > matCallTranscript[0].Source.ToLine {
					t.Fatalf("item %d does not cite the passage it came from: %v", i, err)
				}
				if strings.Contains(it.Text, "ignore previous") {
					t.Fatalf("item %d carries the injected text", i)
				}
			}
			if len(model.reqs) != 1 || model.reqs[0].Kind != ai.KindToolUse || len(model.reqs[0].Tools) != 9 {
				t.Fatalf("model requests = %d, want one tool-use call offering the 9 feature tools", len(model.reqs))
			}
			if !strings.HasPrefix(model.reqs[0].System, feature.Preamble(matter.FeatureCallSummary)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			if md := model.reqs[0].Metadata; md["acting_for"] != matCallRun.ActingFor {
				t.Fatalf("run does not act for the preparing user")
			}
			for _, s := range matCallTranscript {
				matQuotedOnly(t, model.reqs, s.Text)
			}
			// rule: AI-R-005
			for _, s := range []string{matCallTranscript[0].Text, matInjected, "mediation brief"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries transcript or output text")
				}
			}
		})
	}
}

func TestCallSummaryPipelineRefusesBadInput(t *testing.T) {
	tests := []struct {
		name       string
		p          run.Principal
		transcript []agenttools.Quoted
		wantErr    error
	}{
		{"user principal", run.Principal{Kind: registry.PrincipalUser, UserID: "user-lawyer-1"}, matCallTranscript, run.ErrNotAgent},
		{"passage without citation", matCallRun, []agenttools.Quoted{{Text: "transcript"}}, agenttools.ErrCitation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &matRecorder{ModelClient: matScripted(t, matter.FeatureCallSummary, matCallReply(matCallNote))}
			sink := &matCallSink{}
			p := matter.CallSummaryPipeline{Runner: run.New(model, agenttoolsfake.New()), Sink: sink}
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
func TestAI_R_003_CallSummaryDef(t *testing.T) {
	matCheckDefMatchesSpec(t, matter.CallSummaryDef(), false)
	matCheckPrompt(t, matter.CallSummaryDef())
	if len(matter.CallSummaryDef().OutputSchema) == 0 {
		t.Fatal("tool-use feature without output schema")
	}
}
