package run_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
)

// rule: ARC-I-038
func TestARC_I_038_CheckAnswerCitations(t *testing.T) {
	ran := run.Outcome{Citations: []agenttools.Citation{cite, {VersionID: "ver-2", Page: 4, FromLine: 1, ToLine: 9}}}
	tests := []struct {
		name     string
		spec     run.Spec
		json     string
		text     string
		wantErr  error
		wantCite int
	}{
		{name: "cites run result", json: `{"answer":"Due on the 3rd.","citations":[{"version_id":"ver-1","page":1,"from_line":2,"to_line":3}]}`, wantCite: 1},
		{name: "sub-range of run result", json: `{"answer":"A.","citations":[{"version_id":"ver-2","page":4,"from_line":3,"to_line":5}]}`, wantCite: 1},
		{name: "answer in text", text: `{"answer":"A.","citations":[{"version_id":"ver-1","page":1,"from_line":2,"to_line":3}]}`, wantCite: 1},
		{name: "citation not from this run", json: `{"answer":"A.","citations":[{"version_id":"ver-9","page":1,"from_line":2,"to_line":3}]}`, wantErr: run.ErrUncitedAnswer},
		{name: "wrong page", json: `{"answer":"A.","citations":[{"version_id":"ver-1","page":2,"from_line":2,"to_line":3}]}`, wantErr: run.ErrUncitedAnswer},
		{name: "lines outside run result", json: `{"answer":"A.","citations":[{"version_id":"ver-1","page":1,"from_line":2,"to_line":4}]}`, wantErr: run.ErrUncitedAnswer},
		{name: "invalid shape", json: `{"answer":"A.","citations":[{"version_id":"ver-1","page":0,"from_line":2,"to_line":3}]}`, wantErr: agenttools.ErrCitation},
		{name: "invalid shape is uncited", json: `{"answer":"A.","citations":[{"version_id":"","page":1,"from_line":2,"to_line":3}]}`, wantErr: run.ErrUncitedAnswer},
		{name: "not json", text: "Due on the 3rd.", wantErr: run.ErrBadOutput},
		{name: "empty answer", json: `{"answer":" ","citations":[]}`, wantErr: run.ErrBadOutput},
		{name: "unknown field", json: `{"answer":"A.","citations":[],"approve":true}`, wantErr: run.ErrBadOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ran
			out.JSON, out.Text = []byte(tt.json), tt.text
			got, err := run.CheckAnswer(tt.spec, out)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if strings.Contains(err.Error(), "Due on") {
					t.Fatalf("error carries answer text: %v", err)
				}
				return
			}
			if len(got.Citations) != tt.wantCite || got.Text == "" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_AnswerUncitedOnlyWhenSpecAllows(t *testing.T) {
	const noFacts = `{"answer":"I can help with that.","citations":[]}`
	tests := []struct {
		name    string
		spec    run.Spec
		wantErr error
	}{
		{name: "default requires citations", spec: run.Spec{FeatureID: feature}, wantErr: run.ErrUncitedAnswer},
		{name: "spec allows uncited", spec: run.Spec{FeatureID: feature, AnswerMayBeUncited: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := run.CheckAnswer(tt.spec, run.Outcome{JSON: []byte(noFacts)})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_AnswerNotLogged(t *testing.T) {
	const secret = "The limitation period ends on 14 March"
	const fixture = `synthetic: true
feature: F-RUN-01
match:
  last_user_regex: "."
turns:
  - reply: {tool_calls: [{name: read_note, input: {}}]}
  - reply: {json: {answer: "The limitation period ends on 14 March", citations: [{version_id: ver-1, page: 1, from_line: 2, to_line: 3}]}, stop_reason: end_turn}
`
	tests := []struct {
		name string
		spec run.Spec
	}{
		{name: "with answer schema", spec: run.Spec{FeatureID: feature, Grant: []string{"note.read"}, OutputSchema: run.AnswerSchema}},
		{name: "without schema", spec: run.Spec{FeatureID: feature, Grant: []string{"note.read"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			out, err := run.New(model(fixture), tools(t)).Run(context.Background(), tt.spec, agent("run-ans"), "When does the period end?")
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			ans, err := run.CheckAnswer(tt.spec, out)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			if ans.Text != secret {
				t.Fatalf("answer = %q, want the model's answer returned to the caller", ans.Text)
			}
			if logs.Len() == 0 {
				t.Fatal("expected the run to log metadata")
			}
			if strings.Contains(logs.String(), "limitation period") {
				t.Fatalf("slog output carries answer text: %s", logs.String())
			}
		})
	}
}
