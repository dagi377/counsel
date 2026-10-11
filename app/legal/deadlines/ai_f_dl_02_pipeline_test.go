package deadlines

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Synthetic intake facts; the second carries an injection.
var intakeFacts = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-intake-1", Page: 1, FromLine: 2, ToLine: 6}, Text: "The client slipped on ice at the store entrance on 2025-01-14 and learned of the fracture on 2025-01-20."},
	{Source: agenttools.Citation{VersionID: "ver-intake-1", Page: 1, FromLine: 7, ToLine: 8}, Text: injected},
}

var lawyerRun = run.Principal{Kind: registry.PrincipalAgent, RunID: "run-dl-02", ActingFor: "user-lawyer-1"}

const (
	assessCite = `{"version_id": "ver-intake-1", "page": 1, "from_line": 2, "to_line": 6}`
	assessItem = `{"title": "Limitation: negligence claim", "trigger_date": "2025-01-20", "period": "2 years", "citation": ` + assessCite + `}`
)

func assessReply(items string) string {
	return `{"json": {"summary": "Two-year basic period from discovery.", "citations": [` + assessCite + `], "items": [` + items + `]}}`
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
func TestARC_I_038_LimitationAssessmentPipeline(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		opts      []aifake.Option
		wantErr   error
		wantItems int
	}{
		{"cited assessment and deadline", assessReply(assessItem), nil, nil, 1},
		{"injection stays quoted and adds no item", assessReply(assessItem), nil, nil, 1},
		{"assessment without deadlines", assessReply(""), nil, nil, 0},
		{"invalid model json", `{"text": "The period is two years."}`, nil, run.ErrBadOutput, 0},
		{"fails schema", `{"json": {"summary": "x", "citations": [], "items": []}}`, nil, run.ErrBadOutput, 0},
		{"summary cites an unquoted source", `{"json": {"summary": "x", "citations": [{"version_id": "ver-other", "page": 1, "from_line": 2, "to_line": 3}], "items": []}}`, nil, run.ErrUncitedAnswer, 0},
		{"item cites outside the source lines", assessReply(`{"title": "Limitation", "due_date": "2027-01-20", "citation": {"version_id": "ver-intake-1", "page": 1, "from_line": 1, "to_line": 6}}`), nil, run.ErrUncitedAnswer, 0},
		{"item without citation", assessReply(`{"title": "Limitation", "due_date": "2027-01-20"}`), nil, run.ErrBadOutput, 0},
		{"not a calendar date", assessReply(`{"title": "Limitation", "due_date": "2027-02-30", "citation": ` + assessCite + `}`), nil, ErrBadProposal, 0},
		{"rate limited queues a retry", assessReply(assessItem), []aifake.Option{aifake.WithFault(FeatureLimitationAssessment, aifake.FaultRateLimit)}, ErrRetryQueued, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			fake := scripted(t, FeatureLimitationAssessment, tc.reply, tc.opts...)
			model := &recorder{ModelClient: fake}
			sink := &proposals{}
			p := AssessPipeline{Runner: run.New(model, agenttoolsfake.New()), Proposer: sink}

			as, err := p.Run(context.Background(), lawyerRun, intakeFacts)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// Nothing is written when the output does not check out.
				if as.Summary != "" || as.Deadlines != nil || sink.calls != 0 {
					t.Fatalf("wrote %d items in %d calls on error", len(sink.items), sink.calls)
				}
				return
			}
			if len(as.Deadlines) != tc.wantItems || len(sink.items) != tc.wantItems || len(as.Citations) != 1 {
				t.Fatalf("items = %d, written %d, citations %d; want %d items", len(as.Deadlines), len(sink.items), len(as.Citations), tc.wantItems)
			}
			for i, it := range sink.items {
				// rule: ARC-I-038
				if it.Citation.VersionID != intakeFacts[0].Source.VersionID || it.Citation.FromLine > intakeFacts[0].Source.ToLine {
					t.Fatalf("item %d does not cite the fact it came from", i)
				}
			}
			if len(model.reqs) != 1 || model.reqs[0].Kind != ai.KindToolUse || len(model.reqs[0].Tools) != 8 {
				t.Fatalf("model requests = %d, want one tool-use call offering the 8 feature tools", len(model.reqs))
			}
			if !strings.HasPrefix(model.reqs[0].System, feature.Preamble(FeatureLimitationAssessment)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			if md := model.reqs[0].Metadata; md["acting_for"] != lawyerRun.ActingFor {
				t.Fatalf("run does not act for the preparing user")
			}
			for _, f := range intakeFacts {
				quotedOnly(t, model.reqs, f.Text)
			}
			// rule: AI-R-005
			for _, s := range []string{intakeFacts[0].Text, injected, "Two-year basic period", "negligence claim"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries fact or output text")
				}
			}
		})
	}
}

func TestLimitationAssessmentPipelineRefusesBadInput(t *testing.T) {
	tests := []struct {
		name    string
		p       run.Principal
		facts   []agenttools.Quoted
		wantErr error
	}{
		{"user principal", run.Principal{Kind: registry.PrincipalUser, UserID: "user-lawyer-1"}, intakeFacts, run.ErrNotAgent},
		{"fact without citation", lawyerRun, []agenttools.Quoted{{Text: "facts"}}, agenttools.ErrCitation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := scripted(t, FeatureLimitationAssessment, assessReply(assessItem))
			model := &recorder{ModelClient: fake}
			sink := &proposals{}
			p := AssessPipeline{Runner: run.New(model, agenttoolsfake.New()), Proposer: sink}
			if _, err := p.Run(context.Background(), tt.p, tt.facts); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(model.reqs) != 0 || sink.calls != 0 {
				t.Fatalf("model called %d times, wrote %d", len(model.reqs), sink.calls)
			}
		})
	}
}

// rule: AI-R-003
func TestAI_R_003_LimitationAssessmentDef(t *testing.T) {
	checkDef(t, LimitationAssessmentDef())
}
