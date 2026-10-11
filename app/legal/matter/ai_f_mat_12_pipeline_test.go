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
)

// Synthetic matter passages; the second carries an injection.
var matChronoSources = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-letter-1", Page: 1, FromLine: 3, ToLine: 8}, Text: "On 2026-03-02 the landlord served a notice of default for unpaid rent."},
	{Source: agenttools.Citation{VersionID: "ver-letter-1", Page: 2, FromLine: 1, ToLine: 2}, Text: matInjected},
	{Source: agenttools.Citation{VersionID: "ver-email-4", Page: 1, FromLine: 1, ToLine: 4}, Text: "The lease was signed on 2024-05-15, not 2024-06-01 as the intake records."},
}

// matChronoSink records what the pipeline proposes.
type matChronoSink struct {
	calls int
	items []matter.ChronologyItem
}

func (s *matChronoSink) ProposeChronology(_ context.Context, featureID string, items []matter.ChronologyItem) error {
	if featureID != matter.FeatureChronology {
		return errors.New("wrong feature id")
	}
	s.calls++
	s.items = append(s.items, items...)
	return nil
}

var (
	matChronoRow     = `{"kind": "row", "date": "2026-03-02", "text": "Notice of default served", "citation": ` + matCite(agenttools.Citation{VersionID: "ver-letter-1", Page: 1, FromLine: 3, ToLine: 4}) + `}`
	matChronoKeyFact = `{"kind": "key_fact", "date": "2024-05-15", "text": "Check the lease date key fact", "citation": ` + matCite(agenttools.Citation{VersionID: "ver-email-4", Page: 1, FromLine: 1, ToLine: 4}) + `}`
)

func matChronoReply(items ...string) string {
	return `{"json": {"items": [` + strings.Join(items, ", ") + `]}}`
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
func TestARC_I_038_ChronologyPipeline(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		opts      []aifake.Option
		wantErr   error
		wantKinds []string
	}{
		{"cited row and key-fact check", matChronoReply(matChronoRow, matChronoKeyFact), nil, nil, []string{matter.ItemChronologyRow, matter.ItemKeyFactCheck}},
		{"injection stays quoted and adds no item", matChronoReply(matChronoRow), nil, nil, []string{matter.ItemChronologyRow}},
		{"no events", matChronoReply(), nil, nil, nil},
		{"invalid model json", `{"text": "The notice was served in March."}`, nil, run.ErrExtractionSchema, nil},
		{"fails schema", matChronoReply(`{"kind": "row", "date": "2 March", "text": "Notice", "citation": ` + matCite(matChronoSources[0].Source) + `}`), nil, run.ErrExtractionSchema, nil},
		{"one uncited item writes nothing", matChronoReply(matChronoRow, `{"kind": "row", "date": "2026-03-02", "text": "Notice"}`), nil, run.ErrExtractionSchema, nil},
		{"cites a passage it was not given", matChronoReply(`{"kind": "row", "date": "2026-03-02", "text": "Notice", "citation": {"version_id": "ver-other", "page": 1, "from_line": 3, "to_line": 4}}`), nil, run.ErrUncitedAnswer, nil},
		{"row without date", matChronoReply(`{"kind": "row", "text": "Notice", "citation": ` + matCite(matChronoSources[0].Source) + `}`), nil, matter.ErrBadItem, nil},
		{"date not on the calendar", matChronoReply(`{"kind": "row", "date": "2026-02-30", "text": "Notice", "citation": ` + matCite(matChronoSources[0].Source) + `}`), nil, matter.ErrBadItem, nil},
		{"rate limited", matChronoReply(), []aifake.Option{aifake.WithFault(matter.FeatureChronology, aifake.FaultRateLimit)}, ai.ErrRateLimited, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := matCaptureLogs(t)
			fake := matScripted(t, matter.FeatureChronology, tc.reply, tc.opts...)
			model := &matRecorder{ModelClient: fake}
			sink := &matChronoSink{}
			p := matter.ChronologyPipeline{Loop: run.New(model, agenttoolsfake.New()), Embedder: fake, Sink: sink}

			items, err := p.Run(context.Background(), matChronoSources)
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
				if it.Kind != tc.wantKinds[i] {
					t.Fatalf("item %d kind = %s, want %s", i, it.Kind, tc.wantKinds[i])
				}
				// rule: ARC-I-038
				if err := agenttools.ValidCitation(it.Citation); err != nil {
					t.Fatalf("item %d citation: %v", i, err)
				}
				if it.Citation.VersionID == matChronoSources[1].Source.VersionID && it.Citation.Page == matChronoSources[1].Source.Page {
					t.Fatalf("item %d cites the injected passage", i)
				}
			}
			if len(model.reqs) != 1 || model.reqs[0].Kind != ai.KindExtraction || len(model.reqs[0].Tools) != 0 {
				t.Fatalf("model requests = %d, want one extraction call without tools", len(model.reqs))
			}
			if !strings.HasPrefix(model.reqs[0].System, feature.Preamble(matter.FeatureChronology)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			for _, s := range matChronoSources {
				matQuotedOnly(t, model.reqs, s.Text)
			}
			if e := fake.EmbedJournal(); len(e) != 1 || e[0].FeatureID != matter.FeatureChronology || e[0].Inputs != len(matChronoSources)+1 {
				t.Fatalf("embed journal = %+v, want one call of %d inputs", e, len(matChronoSources)+1)
			}
			// rule: AI-R-005
			for _, s := range []string{matChronoSources[0].Text, matInjected, "Notice of default served"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries passage or output text")
				}
			}
		})
	}
}

func TestChronologyPipelineRanksAndBounds(t *testing.T) {
	tests := []struct {
		name      string
		sources   int
		wantQuote int
		wantCall  bool
	}{
		{"no passages, no model call", 0, 0, false},
		{"under the bound", 3, 3, true},
		{"over the bound", matter.MaxChronologySources + 4, matter.MaxChronologySources, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := make([]agenttools.Quoted, tt.sources)
			for i := range src {
				src[i] = agenttools.Quoted{Source: agenttools.Citation{VersionID: "ver-1", Page: i + 1, FromLine: 1, ToLine: 2}, Text: "passage " + strings.Repeat("x", i)}
			}
			fake := matScripted(t, matter.FeatureChronology, matChronoReply())
			model := &matRecorder{ModelClient: fake}
			p := matter.ChronologyPipeline{Loop: run.New(model, agenttoolsfake.New()), Embedder: fake, Sink: &matChronoSink{}}
			if _, err := p.Run(context.Background(), src); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := len(model.reqs) == 1; got != tt.wantCall {
				t.Fatalf("model called = %v, want %v", got, tt.wantCall)
			}
			if !tt.wantCall {
				if len(fake.EmbedJournal()) != 0 {
					t.Fatalf("embedder called with no passages")
				}
				return
			}
			n := 0
			for _, b := range model.reqs[0].Messages[0].Content {
				if b.Type == "tool_result" {
					n++
				}
			}
			if n != tt.wantQuote {
				t.Fatalf("quoted sources = %d, want %d", n, tt.wantQuote)
			}
		})
	}
}

// rule: AI-R-003
func TestAI_R_003_ChronologyDef(t *testing.T) {
	matCheckDefMatchesSpec(t, matter.ChronologyDef(), true)
	matCheckPrompt(t, matter.ChronologyDef())
	if len(matter.ChronologyDef().OutputSchema) == 0 {
		t.Fatal("extraction feature without output schema")
	}
}
