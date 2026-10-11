package feature

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/ai"
)

var prompts = fstest.MapFS{"prompts/f-tim-01.md": {Data: []byte("Propose time entries for the day.\n")}}

func timeDef() Def {
	return Def{
		ID: "F-TIM-01", Area: "time", Name: "Time capture", Source: SourceJob,
		Trigger: "schedule (daily)", Actions: []string{"timesheet > Suggest entries"},
		Tools:     []string{"report_plan", "list_work_items", "propose_time_entries"},
		GateKinds: []string{"time.entries"}, Grant: []string{"time.draft", "work.view"},
		Prompt: prompts, PromptPath: "prompts/f-tim-01.md", Fixture: "testdata/ai/f-tim-01/capture",
	}
}

func TestARC_I_035_AddRejectsBadDefs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Def)
	}{
		{"no id", func(d *Def) { d.ID = "" }},
		{"bad source", func(d *Def) { d.Source = "webhook" }},
		{"no prompt", func(d *Def) { d.Prompt = nil }},
		{"no prompt path", func(d *Def) { d.PromptPath = "" }},
		// rule: ARC-I-035
		{"write tool without gate kinds", func(d *Def) { d.GateKinds = nil }},
		{"unknown tool", func(d *Def) { d.Tools = append(d.Tools, "send_email") }},
		{"repeated tool", func(d *Def) { d.Tools = append(d.Tools, "report_plan") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := timeDef()
			tt.edit(&d)
			if err := NewSet().Add(d); !errors.Is(err, ErrBadDef) {
				t.Fatalf("Add = %v, want ErrBadDef", err)
			}
			if _, err := d.Spec(); !errors.Is(err, ErrBadDef) {
				t.Fatalf("Spec = %v, want ErrBadDef", err)
			}
		})
	}
}

func TestARC_I_035_SetAddByIDAll(t *testing.T) {
	read := Def{ID: "F-ACC-01", Area: "access", Name: "Duty scan", Source: SourceJob,
		Kind: ai.KindChat, Tools: []string{"search_matters"}, Prompt: prompts, PromptPath: "prompts/f-tim-01.md"}
	tests := []struct {
		name    string
		add     []Def
		wantErr error
		wantIDs []string
	}{
		{"read-only feature needs no gate kind", []Def{read}, nil, []string{"F-ACC-01"}},
		{"sorted by id", []Def{timeDef(), read}, nil, []string{"F-ACC-01", "F-TIM-01"}},
		{"duplicate id", []Def{timeDef(), timeDef()}, ErrBadDef, []string{"F-TIM-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Set // the zero value is usable
			var err error
			for _, d := range tt.add {
				if e := s.Add(d); e != nil {
					err = e
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Add = %v, want %v", err, tt.wantErr)
			}
			var ids []string
			for _, d := range s.All() {
				ids = append(ids, d.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tt.wantIDs, ",") {
				t.Fatalf("All = %v, want %v", ids, tt.wantIDs)
			}
			for _, id := range tt.wantIDs {
				if d, ok := s.ByID(id); !ok || d.ID != id {
					t.Fatalf("ByID(%s) = %v, %v", id, d.ID, ok)
				}
			}
			if _, ok := s.ByID("F-NONE-01"); ok {
				t.Fatal("ByID of an unknown id found a definition")
			}
		})
	}
}

func TestARC_I_035_Spec(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	tests := []struct {
		name     string
		edit     func(*Def)
		wantKind ai.CallKind
	}{
		{"default kind is tool-use", func(*Def) {}, ai.KindToolUse},
		{"extraction keeps kind and schema", func(d *Def) { d.Kind = ai.KindExtraction; d.OutputSchema = schema }, ai.KindExtraction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := timeDef()
			tt.edit(&d)
			s, err := d.Spec()
			if err != nil {
				t.Fatal(err)
			}
			if s.FeatureID != d.ID || s.Kind != tt.wantKind {
				t.Fatalf("spec id/kind = %s/%s", s.FeatureID, s.Kind)
			}
			// The shared prompt rules come after the preamble and before the feature prompt.
			want := "feature: F-TIM-01\n" + Rules() + "\nPropose time entries for the day.\n"
			if s.System != want {
				t.Fatalf("System = %q, want %q", s.System, want)
			}
			if string(s.OutputSchema) != string(d.OutputSchema) {
				t.Fatalf("OutputSchema = %s", s.OutputSchema)
			}
			if strings.Join(s.GateKinds, ",") != "time.entries" || strings.Join(s.Grant, ",") != "time.draft,work.view" {
				t.Fatalf("gate kinds %v grant %v", s.GateKinds, s.Grant)
			}
			if len(s.Tools) != len(d.Tools) {
				t.Fatalf("tools = %d, want %d", len(s.Tools), len(d.Tools))
			}
			for i, tool := range s.Tools {
				a2, err := schemas.ByName(d.Tools[i])
				if err != nil {
					t.Fatal(err)
				}
				if tool.Name != a2.Name || string(tool.InputSchema) != string(a2.Input) {
					t.Fatalf("tool %d = %s, want the a2 schema of %s", i, tool.Name, a2.Name)
				}
			}
		})
	}
}

func TestSpecMissingPromptFile(t *testing.T) {
	d := timeDef()
	d.PromptPath = "prompts/missing.md"
	if _, err := d.Spec(); err == nil || errors.Is(err, ErrBadDef) {
		t.Fatalf("Spec = %v, want a prompt read error", err)
	}
}
