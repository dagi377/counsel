package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	draftMatter  = "11111111-1111-4111-8111-111111111111"
	walledMatter = "22222222-2222-4222-8222-222222222222"
	draftDocID   = "33333333-3333-4333-8333-333333333333"
	draftVersion = "44444444-4444-4444-8444-444444444444"
	otherVersion = "55555555-5555-4555-8555-555555555555"
)

var quoteHash = "sha256:" + strings.Repeat("ab", 32)

// fakeDocDrafts stores draft documents in memory.
type fakeDocDrafts struct {
	mu     sync.Mutex
	drafts []agent.NewDraftDocument
}

func (f *fakeDocDrafts) CreateDraft(_ context.Context, d agent.NewDraftDocument) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drafts = append(f.drafts, d)
	return "draft-" + string(rune('0'+len(f.drafts))), nil
}

// fakeRunScope holds the citations each run returned and records the drafts it made.
type fakeRunScope struct {
	mu       sync.Mutex
	returned map[string][]agenttools.Citation
	drafts   map[string][]string
}

func newRunScope() *fakeRunScope {
	return &fakeRunScope{returned: map[string][]agenttools.Citation{}, drafts: map[string][]string{}}
}

func (f *fakeRunScope) Returned(_ context.Context, runID string, c agenttools.Citation) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.returned[runID] {
		if r.VersionID == c.VersionID && r.Page == c.Page && r.FromLine <= c.FromLine && c.ToLine <= r.ToLine {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRunScope) RecordDraft(_ context.Context, runID, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drafts[runID] = append(f.drafts[runID], ref)
	return nil
}

type draftDocWorld struct {
	reg    *agenttools.Registry
	drafts *fakeDocDrafts
	scope  *fakeRunScope
}

// newDraftDocWorld registers the real draft_document on a Registry over module fakes. The
// run "run-doc" has a plan and returned lines 1..10 of page 2 of draftVersion; "run-noplan"
// has no plan. clerk holds actorCaps and is walled from walledMatter.
func newDraftDocWorld(t *testing.T, actorCaps map[string]bool) draftDocWorld {
	t.Helper()
	ctx := context.Background()
	walls := rolesfake.New()
	walls.Wall(clerk, walledMatter)
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	w := draftDocWorld{drafts: &fakeDocDrafts{}, scope: newRunScope()}
	w.scope.returned["run-doc"] = []agenttools.Citation{{VersionID: draftVersion, Page: 2, FromLine: 1, ToLine: 10}}
	h := agent.DraftDocumentHandler()
	if h.Name != agent.ToolDraftDocument {
		t.Fatalf("handler name = %q", h.Name)
	}
	tool, err := h.Build(agent.NewDeps(can, walls, w.drafts, w.scope))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	plans := agenttoolsfake.New()
	if _, err := plans.ReportPlan(ctx, "run-doc", []string{"draft the memo"}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	w.reg = agenttools.NewRegistry(plans)
	if err := w.reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return w
}

func docInput(matterID, extra string, cites ...string) string {
	return `{"matter_id":"` + matterID + `","title":"Synthetic memo","body_markdown":"Body [1]","citations":[` +
		strings.Join(cites, ",") + `]` + extra + `}`
}

func docCite(version string, page, from, to int) string {
	b, _ := json.Marshal(map[string]any{"anchor": "1", "citation": map[string]any{
		"doc_id": draftDocID, "version_id": version, "page": page, "line_start": from, "line_end": to, "quote_hash": quoteHash,
	}})
	return string(b)
}

// rule: ARC-I-035
// rule: ARC-I-036
// rule: ARC-I-039
func TestARC_I_035_DraftDocument(t *testing.T) {
	edit := map[string]bool{"documents.edit": true}
	grant := []string{"documents.edit"}
	agentP := func(run string, g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: clerk, Grant: g}
	}
	cases := []struct {
		name      string
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantCites int
	}{
		{"agent drafts with a citation of its run", agentP("run-doc", grant), edit, docInput(draftMatter, "", docCite(draftVersion, 2, 3, 4)), nil, 1},
		{"agent drafts with no citations", agentP("run-doc", grant), edit, docInput(draftMatter, ""), nil, 0},
		{"manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, edit, docInput(draftMatter, "", docCite(otherVersion, 1, 1, 1)), nil, 1},
		{"citation not returned by this run", agentP("run-doc", grant), edit, docInput(draftMatter, "", docCite(otherVersion, 2, 3, 4)), agent.ErrUncitedSource, 0},
		{"citation past the returned lines", agentP("run-doc", grant), edit, docInput(draftMatter, "", docCite(draftVersion, 2, 9, 11)), agent.ErrUncitedSource, 0},
		{"citation with reversed lines", agentP("run-doc", grant), edit, docInput(draftMatter, "", docCite(draftVersion, 2, 4, 3)), agenttools.ErrCitation, 0},
		{"walled matter", agentP("run-doc", grant), edit, docInput(walledMatter, ""), agent.ErrNotVisible, 0},
		{"walled matter on the manual path", registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}, edit, docInput(walledMatter, ""), agent.ErrNotVisible, 0},
		{"run without the capability", agentP("run-doc", nil), edit, docInput(draftMatter, ""), agenttools.ErrMissingCapability, 0},
		{"acting user without the capability", agentP("run-doc", grant), map[string]bool{}, docInput(draftMatter, ""), agent.ErrForbidden, 0},
		{"no report_plan in the run", agentP("run-noplan", grant), edit, docInput(draftMatter, ""), agenttools.ErrPlanRequired, 0},
		{"title missing", agentP("run-doc", grant), edit, `{"matter_id":"` + draftMatter + `","body_markdown":"x"}`, agenttools.ErrInvalidInput, 0},
		{"unknown field", agentP("run-doc", grant), edit, docInput(draftMatter, `,"send":true`), agenttools.ErrInvalidInput, 0},
		{"title too long", agentP("run-doc", grant), edit, `{"matter_id":"` + draftMatter + `","title":"` + strings.Repeat("t", 201) + `","body_markdown":"x"}`, agenttools.ErrInvalidInput, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newDraftDocWorld(t, tc.actorCaps)
			res, err := w.reg.Call(context.Background(), tc.p, agent.ToolDraftDocument, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if len(w.drafts.drafts) != 0 {
					t.Errorf("refused call stored %d drafts", len(w.drafts.drafts))
				}
				return
			}
			// rule: ARC-I-035
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			var out agenttools.WriteOutput
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatalf("output: %v", err)
			}
			// rule: ARC-I-039
			// A draft opens no gate, so there is nothing for the agent to decide.
			if out.Outcome != agenttools.OutcomeDraft || res.GateID != "" {
				t.Errorf("result = %s gate %q, want a draft and no gate", res.Output, res.GateID)
			}
			if len(w.drafts.drafts) != 1 {
				t.Fatalf("drafts = %d, want 1", len(w.drafts.drafts))
			}
			d := w.drafts.drafts[0]
			if d.PreparedBy != clerk || d.MatterID != draftMatter || len(d.Citations) != tc.wantCites || len(res.Citations) != tc.wantCites {
				t.Errorf("draft = %+v, citations %v", d, res.Citations)
			}
			if err := agenttools.CheckCitations(res); err != nil {
				t.Errorf("citations: %v", err)
			}
			recorded := w.scope.drafts[tc.p.RunID]
			if tc.p.Kind == registry.PrincipalAgent && !slices.Equal(recorded, []string{out.Ref}) {
				t.Errorf("scope drafts = %v, want [%s]", recorded, out.Ref)
			}
		})
	}
}

// rule: ARC-I-036
func TestARC_I_036_DraftDocumentBuildNeedsDeps(t *testing.T) {
	can := agent.CapabilityCheck(func(context.Context, string, string) (bool, error) { return true, nil })
	cases := []struct {
		name string
		deps agent.Deps
		ok   bool
	}{
		{"all deps", agent.NewDeps(can, rolesfake.New(), &fakeDocDrafts{}, newRunScope()), true},
		{"no run scope", agent.NewDeps(can, rolesfake.New(), &fakeDocDrafts{}), false},
		{"no drafts", agent.NewDeps(can, rolesfake.New(), newRunScope()), false},
		{"no walls", agent.NewDeps(can, &fakeDocDrafts{}, newRunScope()), false},
		{"no capability check", agent.NewDeps(rolesfake.New(), &fakeDocDrafts{}, newRunScope()), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool, err := agent.DraftDocumentHandler().Build(tc.deps)
			if tc.ok {
				if err != nil || tool.Kind != registry.ToolWrite || tool.Capability != "documents.edit" {
					t.Fatalf("build = %+v, %v", tool.Kind, err)
				}
				return
			}
			if !errors.Is(err, agent.ErrMissingDep) {
				t.Fatalf("err = %v, want ErrMissingDep", err)
			}
		})
	}
}

// firmInput is a draft_document input on the firm subject; matterID, when set, is added too.
func firmInput(matterID, subjectRef string) string {
	in := `{"title":"Intake form: synthetic","body_markdown":"1. Your name (text, required)."`
	if matterID != "" {
		in += `,"matter_id":"` + matterID + `"`
	}
	if subjectRef != "" {
		in += `,"subject_ref":"` + subjectRef + `"`
	}
	return in + "}"
}

// rule: ARC-I-035
// rule: ARC-I-036
// A firm-level draft (the public wording of an intake form, F-INT-12) names no matter and
// borrows none: draft_document stores it only when the run's grant and the acting user both
// hold intake.forms.
func TestARC_I_036_DraftDocumentFirmSubject(t *testing.T) {
	both := map[string]bool{"documents.edit": true, agent.CapIntakeForms: true}
	editOnly := map[string]bool{"documents.edit": true}
	firmGrant := []string{"documents.edit", agent.CapIntakeForms}
	agentP := func(g []string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-doc", ActingFor: clerk, Grant: g}
	}
	user := registry.Principal{Kind: registry.PrincipalUser, UserID: clerk}
	cases := []struct {
		name      string
		p         registry.Principal
		actorCaps map[string]bool
		in        string
		wantErr   error
	}{
		{"agent drafts on the firm", agentP(firmGrant), both, firmInput("", agent.SubjectFirm), nil},
		{"manual firm draft", user, both, firmInput("", agent.SubjectFirm), nil},
		{"grant lacks intake.forms", agentP([]string{"documents.edit"}), both, firmInput("", agent.SubjectFirm), agent.ErrForbidden},
		{"acting user lacks intake.forms", agentP(firmGrant), editOnly, firmInput("", agent.SubjectFirm), agent.ErrForbidden},
		{"manual user lacks intake.forms", user, editOnly, firmInput("", agent.SubjectFirm), agent.ErrForbidden},
		{"firm subject with a matter", agentP(firmGrant), both, firmInput(draftMatter, agent.SubjectFirm), agent.ErrInvalidInput},
		{"no matter and no firm subject", agentP(firmGrant), both, firmInput("", ""), agent.ErrInvalidInput},
		{"no matter and another subject", agentP(firmGrant), both, firmInput("", "conflict.run:synthetic"), agent.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newDraftDocWorld(t, tc.actorCaps)
			res, err := w.reg.Call(context.Background(), tc.p, agent.ToolDraftDocument, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if len(w.drafts.drafts) != 0 {
					t.Errorf("refused call stored %d drafts", len(w.drafts.drafts))
				}
				return
			}
			if err := agenttools.CheckWriteResult(res); err != nil {
				t.Fatalf("write result: %v", err)
			}
			if len(w.drafts.drafts) != 1 {
				t.Fatalf("drafts = %d, want 1", len(w.drafts.drafts))
			}
			if d := w.drafts.drafts[0]; d.MatterID != "" || d.SubjectRef != agent.SubjectFirm || d.PreparedBy != clerk {
				t.Fatalf("draft = %+v, want a firm draft with no matter prepared by %s", d, clerk)
			}
		})
	}
}
