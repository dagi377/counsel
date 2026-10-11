package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

// pltcMail is a synthetic inbound email in three passages; the second carries an injection.
var pltcMail = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 1, ToLine: 4}, Text: "From: opposing counsel. Re: file 2026-0402 Synth v Platform. Please serve your reply by 2026-11-13."},
	{Source: agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 5, ToLine: 6}, Text: pltcInjected},
	{Source: agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 7, ToLine: 9}, Text: "Our call of 2026-10-08 ran 18 minutes on the reply schedule."},
}

var (
	pltcMailFile     = `{"type": "file", "file_number": "2026-0402", "privileged": false, "confidence_ppm": 950000, "citation": ` + pltcCite(agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 1, ToLine: 2}) + `}`
	pltcMailWeak     = `{"type": "file", "file_number": "2026-0402", "privileged": false, "confidence_ppm": 400000, "citation": ` + pltcCite(agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 1, ToLine: 2}) + `}`
	pltcMailPriv     = `{"type": "file", "file_number": "2026-0402", "privileged": true, "confidence_ppm": 990000, "citation": ` + pltcCite(agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 1, ToLine: 2}) + `}`
	pltcMailUnfiled  = `{"type": "file", "confidence_ppm": 990000, "citation": ` + pltcCite(agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 1, ToLine: 2}) + `}`
	pltcMailDeadline = `{"type": "deadline", "title": "Serve reply", "due_date": "2026-11-13", "citation": ` + pltcCite(agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 3, ToLine: 4}) + `}`
	pltcMailTime     = `{"type": "time_entry", "work_date": "2026-10-08", "minutes": 18, "narrative": "Call with opposing counsel re reply schedule", "citation": ` + pltcCite(agenttools.Citation{VersionID: "ver-mail-1", Page: 1, FromLine: 7, ToLine: 9}) + `}`
)

func pltcMailReply(items ...string) string {
	return `{"json": {"items": [` + strings.Join(items, ", ") + `]}}`
}

// pltcMailSink records what the pipeline writes.
type pltcMailSink struct {
	calls int
	got   platform.MailFiled
}

func (s *pltcMailSink) FileMail(_ context.Context, featureID string, m platform.MailFiled) error {
	if featureID != platform.FeatureMailFiling {
		return errors.New("wrong feature id")
	}
	s.calls++
	s.got = m
	return nil
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFPLT02_DefMatchesSpec(t *testing.T) {
	pltcCheckDefMatchesSpec(t, platform.MailFilingDef())
	if len(platform.MailFilingDef().OutputSchema) == 0 {
		t.Fatal("extraction feature without output schema")
	}
}

// rule: AI-R-005
func TestFPLT02_Prompt(t *testing.T) {
	pltcCheckPrompt(t, platform.MailFilingDef())
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
func TestARC_I_038_MailFilingPipeline(t *testing.T) {
	cases := []struct {
		name          string
		reply         string
		opts          []aifake.Option
		wantErr       error
		wantFiled     bool
		wantQueued    bool
		wantDeadlines int
		wantTime      int
	}{
		{"cited filing, deadline and time", pltcMailReply(pltcMailFile, pltcMailDeadline, pltcMailTime), nil, nil, true, false, 1, 1},
		{"injection stays quoted and adds no item", pltcMailReply(pltcMailDeadline), nil, nil, false, false, 1, 0},
		{"weak match goes to the unfiled queue", pltcMailReply(pltcMailWeak), nil, nil, true, true, 0, 0},
		{"privileged goes to the unfiled queue", pltcMailReply(pltcMailPriv), nil, nil, true, true, 0, 0},
		{"no file number goes to the unfiled queue", pltcMailReply(pltcMailUnfiled), nil, nil, true, true, 0, 0},
		{"nothing to file", pltcMailReply(), nil, nil, false, false, 0, 0},
		{"invalid model json", `{"text": "File it under the Synth matter."}`, nil, run.ErrExtractionSchema, false, false, 0, 0},
		{"fails schema", pltcMailReply(`{"type": "deadline", "title": "Serve", "due_date": "13 Nov", "citation": {"version_id": "ver-mail-1", "page": 1, "from_line": 3, "to_line": 4}}`), nil, run.ErrExtractionSchema, false, false, 0, 0},
		{"item without citation", pltcMailReply(pltcMailDeadline, `{"type": "deadline", "title": "Serve", "due_date": "2026-11-13"}`), nil, run.ErrExtractionSchema, false, false, 0, 0},
		{"cites a passage it was not given", pltcMailReply(`{"type": "deadline", "title": "Serve", "due_date": "2026-11-13", "citation": {"version_id": "ver-other", "page": 1, "from_line": 3, "to_line": 4}}`), nil, run.ErrUncitedAnswer, false, false, 0, 0},
		{"cites the injected passage lines outside a source", pltcMailReply(`{"type": "deadline", "title": "Serve", "due_date": "2026-11-13", "citation": {"version_id": "ver-mail-1", "page": 1, "from_line": 4, "to_line": 7}}`), nil, run.ErrUncitedAnswer, false, false, 0, 0},
		{"date not on the calendar", pltcMailReply(`{"type": "deadline", "title": "Serve", "due_date": "2026-02-30", "citation": {"version_id": "ver-mail-1", "page": 1, "from_line": 3, "to_line": 4}}`), nil, platform.ErrBadMailItem, false, false, 0, 0},
		{"two filings", pltcMailReply(pltcMailFile, pltcMailWeak), nil, platform.ErrBadMailItem, false, false, 0, 0},
		{"time entry without narrative", pltcMailReply(`{"type": "time_entry", "work_date": "2026-10-08", "minutes": 18, "citation": {"version_id": "ver-mail-1", "page": 1, "from_line": 7, "to_line": 9}}`), nil, platform.ErrBadMailItem, false, false, 0, 0},
		{"rate limited writes nothing", pltcMailReply(pltcMailFile), []aifake.Option{aifake.WithFault(platform.FeatureMailFiling, aifake.FaultRateLimit)}, ai.ErrRateLimited, false, false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := pltcCaptureLogs(t)
			model := &pltcRecorder{ModelClient: pltcScripted(t, platform.FeatureMailFiling, tc.reply, tc.opts...)}
			sink := &pltcMailSink{}
			p := platform.MailFilingPipeline{Loop: run.New(model, agenttoolsfake.New()), Sink: sink}

			m, err := p.Run(context.Background(), pltcMail)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// Nothing is written when the output does not check out.
				if sink.calls != 0 || m.Filing != nil || len(m.Deadlines) != 0 || len(m.Time) != 0 {
					t.Fatalf("wrote %d times on error", sink.calls)
				}
				return
			}
			wantCalls := 0
			if tc.wantFiled || tc.wantDeadlines > 0 || tc.wantTime > 0 {
				wantCalls = 1
			}
			if sink.calls != wantCalls {
				t.Fatalf("sink calls = %d, want %d", sink.calls, wantCalls)
			}
			got := sink.got
			if (got.Filing != nil) != tc.wantFiled || len(got.Deadlines) != tc.wantDeadlines || len(got.Time) != tc.wantTime {
				t.Fatalf("filed %v, deadlines %d, time %d; want %v, %d, %d", got.Filing != nil, len(got.Deadlines), len(got.Time), tc.wantFiled, tc.wantDeadlines, tc.wantTime)
			}
			if got.Filing != nil && got.Filing.Queued != tc.wantQueued {
				t.Fatalf("queued = %v, want %v", got.Filing.Queued, tc.wantQueued)
			}
			// rule: ARC-I-038
			// Every item cites the passage it came from.
			var cites []agenttools.Citation
			if got.Filing != nil {
				cites = append(cites, got.Filing.Citation)
			}
			for _, d := range got.Deadlines {
				cites = append(cites, d.Citation)
				if strings.Contains(d.Title, "ignore previous") {
					t.Fatal("deadline carries the injected text")
				}
			}
			for _, e := range got.Time {
				cites = append(cites, e.Citation)
			}
			for i, c := range cites {
				if err := agenttools.ValidCitation(c); err != nil || c.VersionID != "ver-mail-1" {
					t.Fatalf("item %d does not cite a quoted passage: %v", i, err)
				}
			}
			if len(model.reqs) != 1 || model.reqs[0].Kind != ai.KindExtraction || len(model.reqs[0].Tools) != 0 {
				t.Fatalf("model requests = %d, want one extraction call offering no tools", len(model.reqs))
			}
			if !strings.HasPrefix(model.reqs[0].System, feature.Preamble(platform.FeatureMailFiling)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			for _, s := range pltcMail {
				pltcQuotedOnly(t, model.reqs, s.Text)
			}
			// rule: AI-R-005
			for _, s := range []string{pltcMail[0].Text, pltcInjected, "2026-0402", "Serve reply"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries mail or output text")
				}
			}
		})
	}
}

func TestMailFilingPipelineRefusesBadInput(t *testing.T) {
	tests := []struct {
		name    string
		sources []agenttools.Quoted
		wantErr error
	}{
		{"passage without citation", []agenttools.Quoted{{Text: "mail"}}, agenttools.ErrCitation},
		{"no passages", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &pltcRecorder{ModelClient: pltcScripted(t, platform.FeatureMailFiling, pltcMailReply(pltcMailFile))}
			sink := &pltcMailSink{}
			p := platform.MailFilingPipeline{Loop: run.New(model, agenttoolsfake.New()), Sink: sink}
			if _, err := p.Run(context.Background(), tt.sources); !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(model.reqs) != 0 || sink.calls != 0 {
				t.Fatalf("model called %d times, wrote %d", len(model.reqs), sink.calls)
			}
		})
	}
}
