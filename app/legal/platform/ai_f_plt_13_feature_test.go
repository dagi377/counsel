package platform_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

// rule: AI-R-003
// rule: ARC-I-035
func TestFPLT13_DefMatchesSpec(t *testing.T) {
	d := platform.FilingScopeReevaluationDef()
	pltCheckDefMatchesSpec(t, d)
	pltiCheckReadOnly(t, d)
}

// rule: AI-R-005
func TestFPLT13_Prompt(t *testing.T) {
	pltCheckPrompt(t, platform.FilingScopeReevaluationDef(), "Filing scope re-evaluation", "read-only", "never-file", "citation")
}

// F-PLT-13 starts when the sync scope changes, acting for the person who asked for the change;
// it has no schedule and no screen action.
func TestFPLT13_TriggerBinding(t *testing.T) {
	b, err := platform.FilingScopeReevaluationBinding()
	pltiCheckBinding(t, b, err, platform.FeatureFilingScopeReevaluation, platform.EventSyncScopeChanged)
	if n := len(platform.FilingScopeReevaluationDef().Actions); n != 0 {
		t.Fatalf("actions = %d, want none", n)
	}
}

// plt13Mail is the filed email the run reads: page 1 of a document on the seeded matter.
var plt13Mail = []string{
	"From: staff member, internal thread on the estate accounts.",
	"Filed under scope contacts_and_staff; outside scope contacts.",
}

// plt13Calls is the F-PLT-13 script: read the matter, find the filed mail, read it.
func plt13Calls(w pltiWorld) []pltCall {
	return []pltCall{
		{agent.ToolGetMatter, map[string]any{"matter_id": w.matter}},
		{agent.ToolSearchDocuments, map[string]any{"matter_id": w.matter, "query": "thread"}},
		{agent.ToolReadDocument, map[string]any{"doc_id": w.doc, "version_id": w.version}},
	}
}

const plt13Preview = "Would unfile: 1 item. The internal staff thread on the estate accounts leaves the matter under scope contacts. Would file: none."

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
func TestFPLT13_FromFeature(t *testing.T) {
	cases := []struct {
		name     string
		version  string // version cited; "" cites the read version
		from, to int
		opts     []aifake.Option
		wantErr  error // run.CheckAnswer
		retry    bool
	}{
		{name: "ok", from: 1, to: 2},
		// rule: ARC-I-038
		{name: "citation not returned", version: "00000000-0000-4000-8000-0000000000ff", from: 1, to: 1, wantErr: run.ErrUncitedAnswer},
		{name: "lines outside the read", from: 2, to: 5, wantErr: run.ErrUncitedAnswer},
		// rule: AI-R-003
		{name: "rate limited queues a retry", from: 1, to: 2, opts: []aifake.Option{aifake.WithFault(platform.FeatureFilingScopeReevaluation, aifake.FaultRateLimit)}, retry: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := pltiFireRead(t, platform.FilingScopeReevaluationDef(), plt13Mail, plt13Calls, func(w pltiWorld) map[string]any {
				v := w.version
				if tc.version != "" {
					v = tc.version
				}
				return pltiAnswer(plt13Preview, v, tc.from, tc.to)
			}, tc.opts...)
			if r.err != nil {
				t.Fatalf("run: %v", r.err)
			}
			// rule: AI-R-005
			// No write and no gate row: the preview is an answer the person may ignore.
			if len(r.out.GateIDs) != 0 || len(r.fd.StandIns) != 0 {
				t.Fatalf("gates %v, stand-ins %v; want none", r.out.GateIDs, r.fd.StandIns)
			}
			if tc.retry {
				if !r.out.RetryQueued || len(r.out.Calls) != 0 {
					t.Fatalf("retry = %v, calls %v; want a queued retry and no call", r.out.RetryQueued, r.out.Calls)
				}
				return
			}
			if want := pltiToolNames(plt13Calls(r.pltiWorld)); !slices.Equal(pltiCalled(r.out), want) {
				t.Fatalf("tools = %v, want %v", pltiCalled(r.out), want)
			}
			for _, c := range r.out.Calls {
				if c.Err != nil {
					t.Fatalf("%s refused: %v", c.Tool, c.Err)
				}
			}
			ans, err := run.CheckAnswer(r.spec, r.out)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("CheckAnswer = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(ans.Citations) != 1 || ans.Citations[0].VersionID != r.version || ans.Text != plt13Preview {
				t.Fatalf("answer = %+v, want the preview citing the read version", ans)
			}
		})
	}
}

// rule: AI-R-003
// The run reads only matters the acting user may see: a walled matter's read is refused and
// nothing of it reaches the model.
func TestFPLT13_WalledMatterRefused(t *testing.T) {
	r := pltiFireRead(t, platform.FilingScopeReevaluationDef(), plt13Mail, func(w pltiWorld) []pltCall {
		walled := w.fd.Seed.Matter("2026-0499", "Synth walled file")
		w.fd.Seed.Wall(string(walled.ID))
		return []pltCall{{agent.ToolGetMatter, map[string]any{"matter_id": string(walled.ID)}}}
	}, func(w pltiWorld) map[string]any { return pltiAnswer(plt13Preview, w.version, 1, 1) })
	if r.err != nil {
		t.Fatalf("run: %v", r.err)
	}
	if len(r.out.Calls) != 1 || r.out.Calls[0].Err == nil {
		t.Fatalf("calls = %+v, want get_matter refused", r.out.Calls)
	}
	for _, req := range r.model.reqs {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if strings.Contains(b.Text, "Synth walled file") {
					t.Fatalf("walled matter reached the model")
				}
			}
		}
	}
	// Nothing was read, so the answer has nothing to cite.
	if _, err := run.CheckAnswer(r.spec, r.out); !errors.Is(err, run.ErrUncitedAnswer) {
		t.Fatalf("CheckAnswer = %v, want ErrUncitedAnswer", err)
	}
}

// rule: ARC-I-038
func TestFPLT13_Injection(t *testing.T) {
	r := pltiFireRead(t, platform.FilingScopeReevaluationDef(), append(slices.Clone(plt13Mail), pltInjected), plt13Calls, func(w pltiWorld) map[string]any {
		return pltiAnswer(plt13Preview+" A document line asks for an action; not acted on.", w.version, 1, 2)
	})
	if r.err != nil {
		t.Fatalf("run: %v", r.err)
	}
	if want := pltiToolNames(plt13Calls(r.pltiWorld)); !slices.Equal(pltiCalled(r.out), want) {
		t.Fatalf("tools = %v, want %v (no extra call)", pltiCalled(r.out), want)
	}
	if err := pltiQuotedOnly(r, pltInjected); err != nil {
		t.Fatal(err)
	}
	ans, err := run.CheckAnswer(r.spec, r.out)
	if err != nil {
		t.Fatalf("CheckAnswer: %v", err)
	}
	// The injected text is in no tool input (pltiQuotedOnly) and not in the answer.
	if strings.Contains(ans.Text, pltInjected) || len(r.out.GateIDs) != 0 {
		t.Fatalf("answer carries the injected text or a gate opened")
	}
	if err := agenttools.ValidCitation(ans.Citations[0]); err != nil {
		t.Fatal(err)
	}
}
