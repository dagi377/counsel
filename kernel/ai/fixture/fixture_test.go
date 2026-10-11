package fixture

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

const okYAML = `synthetic: true
feature: F-TST-01
match:
  last_user_regex: "rebuild day"
latency_ms: 40
fault: null
turns:
  - expect_tools: [report_plan]
    reply:
      tool_calls:
        - name: report_plan
          input: {steps: [{id: rebuild, title: "Rebuild"}]}
      stop_reason: tool_use
      usage: {input_tokens: 812, output_tokens: 64}
  - expect_tool_results: [report_plan]
    reply: {text: "Done.", stop_reason: end_turn}
`

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr error
	}{
		{name: "ok", data: okYAML},
		{name: "fault without turns", data: "synthetic: true\nfeature: F-TST-01\nfault: timeout\n"},
		{name: "no turns no fault", data: "synthetic: true\nfeature: F-TST-01\n", wantErr: ErrInvalid},
		{name: "no feature", data: "synthetic: true\nfault: timeout\n", wantErr: ErrInvalid},
		{name: "unknown fault", data: "synthetic: true\nfeature: F-TST-01\nfault: flaky\n", wantErr: ErrInvalid},
		{name: "unknown key", data: "synthetic: true\nfeature: F-TST-01\nfault: timeout\nlatency: 3\n", wantErr: ErrInvalid},
		{name: "bad regex", data: "synthetic: true\nfeature: F-TST-01\nfault: timeout\nmatch: {last_user_regex: \"(\"}\n", wantErr: ErrInvalid},
		{name: "tool call without name", data: "synthetic: true\nfeature: F-TST-01\nturns: [{reply: {tool_calls: [{input: {}}]}}]\n", wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("x/ok.yaml", []byte(tt.data))
			if tt.wantErr == nil && err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("Parse err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAI_R_004_RejectsNonSynthetic(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing", data: "feature: F-TST-01\nfault: timeout\n"},
		{name: "false", data: "synthetic: false\nfeature: F-TST-01\nfault: timeout\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// rule: AI-R-004
			if _, err := Parse("x/ok.yaml", []byte(tt.data)); !errors.Is(err, ErrNotSynthetic) {
				t.Fatalf("Parse err = %v, want ErrNotSynthetic", err)
			}
		})
	}
}

func TestParseFields(t *testing.T) {
	f, err := Parse("f-tst-01/day/ok.yaml", []byte(okYAML))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "variant", got: f.Variant, want: VariantOK},
		{name: "feature", got: f.Feature, want: "F-TST-01"},
		{name: "latency", got: f.LatencyMS, want: 40},
		{name: "fault", got: f.Fault, want: FaultNone},
		{name: "turns", got: len(f.Turns), want: 2},
		{name: "expect tools", got: strings.Join(f.Turns[0].ExpectTools, ","), want: "report_plan"},
		{name: "tool name", got: f.Turns[0].Reply.ToolCalls[0].Name, want: "report_plan"},
		{name: "tool input json", got: string(f.Turns[0].Reply.ToolCalls[0].Input), want: `{"steps":[{"id":"rebuild","title":"Rebuild"}]}`},
		{name: "usage", got: f.Turns[0].Reply.Usage.InputTokens, want: 812},
		{name: "text", got: f.Turns[1].Reply.Text, want: "Done."},
		{name: "stop", got: f.Turns[1].Reply.StopReason, want: "end_turn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	byHash := Fixture{Match: Match{PromptHash: "abc"}}
	byRegex, err := Parse("x/ok.yaml", []byte(okYAML))
	if err != nil {
		t.Fatal(err)
	}
	both, err := Parse("x/ok.yaml", []byte(strings.Replace(okYAML, "match:\n", "match:\n  prompt_hash: abc\n", 1)))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		f        Fixture
		hash     string
		lastUser string
		want     bool
	}{
		{name: "hash hit", f: byHash, hash: "abc", want: true},
		{name: "hash miss", f: byHash, hash: "def", lastUser: "rebuild day", want: false},
		{name: "regex hit", f: byRegex, hash: "zzz", lastUser: "please rebuild day 3", want: true},
		{name: "regex miss", f: byRegex, lastUser: "other", want: false},
		{name: "stale hash falls back to regex", f: both, hash: "new", lastUser: "rebuild day", want: true},
		{name: "no match keys matches all", f: Fixture{}, hash: "x", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Matches(tt.hash, tt.lastUser); got != tt.want {
				t.Fatalf("Matches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadFeature(t *testing.T) {
	fsys := fstest.MapFS{
		"f-tst-01/day/ok.yaml":         {Data: []byte(okYAML)},
		"f-tst-01/day/timeout.yaml":    {Data: []byte("synthetic: true\nfeature: F-TST-01\nfault: timeout\n")},
		"f-tst-01/day/inputs/seed.txt": {Data: []byte("seed")},
		"f-tst-02/x/ok.yaml":           {Data: []byte("synthetic: true\nfeature: F-TST-01\nfault: timeout\n")},
	}
	tests := []struct {
		name     string
		feature  string
		wantPath []string
		wantErr  error
	}{
		{name: "loads variants, skips inputs", feature: "F-TST-01", wantPath: []string{"f-tst-01/day/ok.yaml", "f-tst-01/day/timeout.yaml"}},
		{name: "feature mismatch", feature: "F-TST-02", wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadFeature(fsys, tt.feature)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, f := range got {
				paths = append(paths, f.Path)
			}
			if strings.Join(paths, ",") != strings.Join(tt.wantPath, ",") {
				t.Fatalf("paths = %v, want %v", paths, tt.wantPath)
			}
		})
	}
}

// TestReadmeExamples keeps testdata/ai/README.md in step with the loader: every fixture
// example in it must parse.
func TestReadmeExamples(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/ai/README.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```yaml\n(.*?)```").FindAllStringSubmatch(string(data), -1)
	if len(blocks) < 3 {
		t.Fatalf("found %d yaml examples, want >= 3", len(blocks))
	}
	for i, b := range blocks {
		if _, err := Parse("README.md", []byte(b[1])); err != nil {
			t.Errorf("example %d: %v", i, err)
		}
	}
}
