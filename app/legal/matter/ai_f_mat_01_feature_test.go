package matter_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/agenttools"
)

// matBriefWorld is one F-MAT-01 feature driver over the app's real handlers with a seeded
// matter and one seeded document.
type matBriefWorld struct {
	fd      *agenttest.FeatureDriver
	matter  string
	doc     string
	version string
}

// matBriefFeature builds F-MAT-01's FeatureDriver from agenttest.FromFeature with an
// approvable gate store, a seeded matter, a client contact and a document holding lines on
// page 1.
func matBriefFeature(t *testing.T, lines []string) matBriefWorld {
	t.Helper()
	fd := agenttest.FromFeature(t, matter.BriefRefreshDef())
	matApprovableGates(fd)
	m := fd.Seed.Matter("2026-0101", "Synth v Example")
	fd.Seed.Contact("Synth Holdings Ltd")
	doc, ver := fd.Seed.Document(string(m.ID), lines)
	return matBriefWorld{fd: fd, matter: string(m.ID), doc: doc, version: string(ver)}
}

// cite is a draft_document citation of line on page 1 of the world's document.
func (w matBriefWorld) cite(version string, line int) map[string]any {
	return map[string]any{"doc_id": w.doc, "version_id": version, "page": 1, "line_start": line, "line_end": line,
		"quote_hash": "sha256:" + strings.Repeat("ab", 32)}
}

// calls is the scripted run: plan, reads, the brief draft, the key-fact draft and its
// submission for gateKind. citeVersion is the version the key-fact draft cites.
func (w matBriefWorld) calls(gateKind, citeVersion string) []matCall {
	draft := func(title, body string, line int, version string) matCall {
		return matCall{agent.ToolDraftDocument, map[string]any{
			"matter_id": w.matter, "title": title, "body_markdown": body,
			"citations": []any{map[string]any{"anchor": "c1", "citation": w.cite(version, line)}},
		}}
	}
	return []matCall{
		matPlan("read", "draft-brief", "key-fact"),
		{"get_matter", map[string]any{"matter_id": w.matter}},
		{"search_documents", map[string]any{"matter_id": w.matter, "query": "lease date"}},
		{"read_document", map[string]any{"doc_id": w.doc, "version_id": w.version}},
		{"search_contacts", map[string]any{"query": "Synth Holdings"}},
		draft("Matter brief", "Lease dispute; the lease was signed earlier than recorded [c1].", 2, w.version),
		draft("Key-fact change: lease date", "Lease date is 2024-05-15, not 2024-06-01 [c1].", 2, citeVersion),
		// The second draft is the run's third seeded record: one document, then two drafts.
		{"submit_for_approval", map[string]any{"subject_type": "document", "subject_id": "draft-document-3", "gate_kind": gateKind}},
	}
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFMAT01_DefMatchesSpec(t *testing.T) {
	matCheckDefMatchesSpec(t, matter.BriefRefreshDef(), false)
}

// rule: AI-R-005
func TestFMAT01_Prompt(t *testing.T) {
	matCheckPrompt(t, matter.BriefRefreshDef())
}

func TestFMAT01_TriggerBinding(t *testing.T) {
	b, err := matter.BriefRefreshBinding()
	matBindingIs(t, b, err, matter.FeatureBriefRefresh,
		matter.EventDocumentUploaded, matter.EventEmailFiled, matter.EventNoteAdded, matter.EventCalendarAdded)
	actions := matter.BriefRefreshDef().Actions
	for _, want := range []string{"matter-overview > Refresh brief", "matter-overview > Propose key-fact change"} {
		if !slices.Contains(actions, want) {
			t.Errorf("on-request action %q not bound", want)
		}
	}
}

// rule: AI-R-003
// rule: ARC-I-035
// rule: ARC-I-038
// rule: ARC-I-039
func TestFMAT01_FromFeature(t *testing.T) {
	lines := []string{"Lease dispute file note.", "The lease was signed on 2024-05-15, not 2024-06-01."}
	tests := []struct {
		name   string
		inject bool
	}{{"ok", false}, {"injection", true}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			l := slices.Clone(lines)
			if tc.inject {
				l = append(l, matInjected)
			}
			w := matBriefFeature(t, l)
			if len(w.fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", w.fd.StandIns)
			}
			calls := w.calls(matter.GateKeyFact, w.version)
			run := w.fd.Fire(ctx, matScript(t, matter.FeatureBriefRefresh, calls, "Brief updated; key-fact change waits for G-KF."), "run-f-mat-01-"+tc.name, "matter "+w.matter)
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			if len(run.Refused) != 0 {
				t.Fatalf("refused = %+v", run.Refused)
			}
			// No extra tool call, in the injection run too.
			if want := matToolNames(calls); !slices.Equal(run.Tools, want) {
				t.Fatalf("tools = %v, want %v", run.Tools, want)
			}
			// rule: ARC-I-039
			if run.PlanAt != 0 || run.FirstWrite != slices.Index(run.Tools, agent.ToolDraftDocument) {
				t.Fatalf("plan at %d, first write %d: report_plan must precede draft_document", run.PlanAt, run.FirstWrite)
			}
			if d := w.fd.Seed.Store.Drafts; len(d) != 2 || d[1].ID != "draft-document-3" || d[1].PreparedBy != agenttest.FeatureActingFor {
				t.Fatalf("drafts = %+v, want the brief and the key-fact draft, prepared by the acting user", d)
			}
			if len(run.GateIDs) != 1 {
				t.Fatalf("gates = %v, want one matter.key_fact gate", run.GateIDs)
			}
			matCheckGate(t, w.fd, run.GateIDs[0], matter.GateKeyFact)
			if !tc.inject {
				return
			}
			// rule: ARC-I-038
			if err := agenttest.QuotedOnly(run.Run, matInjected); err != nil {
				t.Fatal(err)
			}
			if err := matAbsent(ctx, w.fd, run, matInjected); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// rule: ARC-I-035
// rule: ARC-I-038
func TestFMAT01_FromFeatureRefusesWrites(t *testing.T) {
	lines := []string{"Lease dispute file note.", "The lease was signed on 2024-05-15, not 2024-06-01."}
	tests := []struct {
		name     string
		gateKind string
		cite     func(w matBriefWorld) string
		refused  string
		wantErr  error
	}{
		{"gate kind outside the Def", "cal.confirm", func(w matBriefWorld) string { return w.version }, "submit_for_approval", agenttools.ErrGateKindNotAllowed},
		// A draft may cite only what a read of this run returned (the draft tools' unsourced
		// check; no F-MAT-01 write carries an amount or a date field).
		{"key-fact draft cites an unread version", matter.GateKeyFact, func(matBriefWorld) string { return "00000000-0000-4000-8000-0000000000f1" }, agent.ToolDraftDocument, agent.ErrUncitedSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := matBriefFeature(t, lines)
			calls := w.calls(tt.gateKind, tt.cite(w))
			run := w.fd.Fire(context.Background(), matScript(t, matter.FeatureBriefRefresh, calls, "Done."), "run-f-mat-01-refused", "matter "+w.matter)
			i := slices.IndexFunc(run.Refused, func(r agenttest.Refusal) bool { return r.Tool == tt.refused })
			if i < 0 || !errors.Is(run.Refused[i].Err, tt.wantErr) {
				t.Fatalf("refused = %+v, want %s refused with %v", run.Refused, tt.refused, tt.wantErr)
			}
			if len(run.GateIDs) != 0 {
				t.Fatalf("gates = %v, want none opened", run.GateIDs)
			}
		})
	}
}
