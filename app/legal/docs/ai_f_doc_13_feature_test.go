package docs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
)

// F-DOC-13 tests.

// rule: AI-R-003
func TestFDOC13_TriggerBinding(t *testing.T) {
	docgCheckTrigger(t, docs.AutoFileDef(), "matter-documents", "Auto-file")
}

// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-I-038
// The result is one ranked, cited filing suggestion for the screen: no write tool, no gate.
func TestFDOC13_ScreenActionRun(t *testing.T) {
	def := docs.AutoFileDef()
	docgCheckReadOnlyDef(t, def)
	cases := []struct {
		name        string
		grant       []string
		lines       []string
		out         func(matter, version string) map[string]any
		wantRefused error
		wantErr     error
		wantReview  bool
		injected    bool
	}{
		{name: "ok", lines: docgOKLines, out: docgFiling(docgCite{from: 1, to: 2}, 930_000)},
		{name: "low confidence queued for review", lines: docgOKLines, out: docgFiling(docgCite{from: 1, to: 1}, 610_000), wantReview: true},
		{name: "injection quoted only", lines: docgInjectedLines, out: docgFiling(docgCite{from: 1, to: 1}, 900_000), injected: true},
		{name: "line never read", lines: docgOKLines, out: docgFiling(docgCite{from: 1, to: 47}, 930_000), wantErr: agenttools.ErrUnsourcedValue},
		{name: "version never read", lines: docgOKLines, out: docgFiling(docgCite{version: "00000000-0000-4000-8000-0000000000f9", from: 1, to: 2}, 930_000), wantErr: run.ErrUncitedAnswer},
		{name: "403 without documents.view", grant: docgNoGrant(def.Grant, agent.CapDocumentsView), lines: docgOKLines,
			out: docgFiling(docgCite{from: 1, to: 2}, 930_000), wantRefused: agenttools.ErrMissingCapability, wantErr: agenttools.ErrUnsourcedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := docgFire(t, def, tc.grant, tc.lines, tc.out)
			docgCheckRun(t, r, tc.wantRefused)
			sink := &docgSink{}
			items, err := docs.AutoFileFromRun(context.Background(), sink, docgProvenance(r), r.runID, docgOutcome(t, r))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AutoFileFromRun = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// rule: AI-R-003
				if len(sink.items) != 0 {
					t.Fatalf("sink got %d suggestions from a refused output", len(sink.items))
				}
				return
			}
			if len(items) != 1 || len(sink.items) != 1 || items[0].Citation.VersionID != r.version || items[0].Review != tc.wantReview {
				t.Fatalf("items = %+v, sink %d; want one suggestion citing %s, review %v", items, len(sink.items), r.version, tc.wantReview)
			}
			if tc.injected {
				// rule: ARC-I-038
				if err := agenttest.QuotedOnly(r.run.Run, docgInjected); err != nil {
					t.Fatal(err)
				}
				if err := docgAbsent(r, docgInjected); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// rule: AI-R-003
func TestFDOC13_AgentNeverApproves(t *testing.T) {
	docgCheckNoApproval(t, docs.AutoFileDef())
}

// rule: AI-R-003
func TestFDOC13_FromRunRefusesGateRun(t *testing.T) {
	out := run.Outcome{JSON: json.RawMessage(`{"items":[]}`), GateIDs: []string{"g-1"}}
	if _, err := docs.AutoFileFromRun(context.Background(), &docgSink{}, agenttools.NewProvenance(), "run-x", out); !errors.Is(err, docs.ErrBadItem) {
		t.Fatalf("err = %v, want ErrBadItem", err)
	}
}
