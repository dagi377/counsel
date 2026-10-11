package aifake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/kernel/ai"
)

func userReq(feature, text, run string) ai.Request {
	return ai.Request{
		FeatureID: feature,
		Kind:      ai.KindToolUse,
		System:    "You draft entries.",
		Messages:  []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: text}}}},
		Metadata:  map[string]string{"run_id": run},
	}
}

func mustHash(t *testing.T, r ai.Request) string {
	t.Helper()
	h, err := PromptHash(r)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// fixtures builds a testdata/ai tree. The hashed fixture matches userReq(F-X-01, "hashed", *).
func fixtures(t *testing.T) fstest.MapFS {
	t.Helper()
	hash := mustHash(t, userReq("F-X-01", "hashed", ""))
	return fstest.MapFS{
		"f-x-01/case/ok.yaml": {Data: []byte(`synthetic: true
feature: F-X-01
match:
  prompt_hash: ` + hash + `
turns:
  - reply:
      tool_calls:
        - name: report_plan
          input: {steps: [{id: rebuild, title: "Rebuild day"}]}
      stop_reason: tool_use
      usage: {input_tokens: 812, output_tokens: 64}
  - reply: {text: "Proposed 1 entry for your review.", stop_reason: end_turn}
`)},
		"f-x-01/case/regex.yaml": {Data: []byte(`synthetic: true
feature: F-X-01
match:
  last_user_regex: "rebuild day"
turns:
  - reply: {text: "matched by regex"}
`)},
		"f-x-01/case/error_429.yaml": {Data: []byte(`synthetic: true
feature: F-X-01
match:
  last_user_regex: "^retry me$"
fault: rate_limit
turns:
  - reply: {text: "ok after retry"}
`)},
		"f-x-01/case/timeout.yaml": {Data: []byte(`synthetic: true
feature: F-X-01
match:
  last_user_regex: "^slow$"
fault: timeout
turns: []
`)},
		"f-x-01/case/inputs/seed.yaml": {Data: []byte(`rows: []`)},
		"f-x-02/case/ok.yaml": {Data: []byte(`synthetic: true
feature: F-X-02
match:
  last_user_regex: "extract"
turns:
  - reply:
      json: {name: "Synthetic Co", total_cents: 1200}
`)},
	}
}

func TestComplete(t *testing.T) {
	tests := []struct {
		name     string
		req      ai.Request
		calls    int // answer of the N-th call (1-based) is checked
		wantText string
		wantTool string
		wantJSON string
		wantErr  error
		wantFix  string
	}{
		{name: "hash turn 1 tool call", req: userReq("F-X-01", "hashed", "r1"), calls: 1, wantTool: "report_plan", wantFix: "f-x-01/case/ok.yaml"},
		{name: "hash turn 2 text", req: userReq("F-X-01", "hashed", "r1"), calls: 2, wantText: "Proposed 1 entry for your review.", wantFix: "f-x-01/case/ok.yaml"},
		{name: "regex fallback", req: userReq("F-X-01", "please rebuild day 9", "r1"), calls: 1, wantText: "matched by regex", wantFix: "f-x-01/case/regex.yaml"},
		{name: "rate limited then ok", req: userReq("F-X-01", "retry me", "r1"), calls: 2, wantText: "ok after retry"},
		{name: "extraction json", req: userReq("F-X-02", "extract this", ""), calls: 1, wantJSON: `{"name":"Synthetic Co","total_cents":1200}`},
		{name: "unknown hash", req: userReq("F-X-01", "nothing matches", "r1"), calls: 1, wantErr: ai.ErrNoFixture},
		{name: "unknown feature", req: userReq("F-NONE-01", "x", ""), calls: 1, wantErr: ai.ErrNoFixture},
		{name: "no feature", req: userReq("", "x", ""), calls: 1, wantErr: ai.ErrNoFeature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(fixtures(t))
			var resp ai.Response
			var err error
			for range tt.calls {
				resp, err = f.Complete(context.Background(), tt.req)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if resp.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", resp.Text, tt.wantText)
			}
			if tt.wantTool != "" {
				if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != tt.wantTool || resp.ToolCalls[0].ID == "" || resp.StopReason != "tool_use" {
					t.Errorf("ToolCalls = %+v stop %q, want one %s", resp.ToolCalls, resp.StopReason, tt.wantTool)
				}
				if resp.Usage.InputTokens != 812 || resp.Usage.OutputTokens != 64 {
					t.Errorf("Usage = %+v", resp.Usage)
				}
			}
			if string(resp.JSON) != tt.wantJSON {
				t.Errorf("JSON = %s, want %s", resp.JSON, tt.wantJSON)
			}
			if tt.wantFix != "" && resp.Fixture != tt.wantFix {
				t.Errorf("Fixture = %q, want %q", resp.Fixture, tt.wantFix)
			}
		})
	}
}

func TestNoFixtureNamesHashAndNearest(t *testing.T) {
	req := userReq("F-X-01", "nothing matches", "")
	_, err := New(fixtures(t)).Complete(context.Background(), req)
	if !errors.Is(err, ai.ErrNoFixture) {
		t.Fatalf("err = %v, want ErrNoFixture", err)
	}
	for _, want := range []string{mustHash(t, req), "nearest f-x-01/case/"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestFaults(t *testing.T) {
	tests := []struct {
		name       string
		opts       []Option
		text       string
		want       error
		wantStatus int
	}{
		{name: "fixture 429", text: "retry me", want: ai.ErrRateLimited, wantStatus: 429},
		{name: "fixture timeout", text: "slow", want: ai.ErrTimeout, wantStatus: 0},
		{name: "overloaded", opts: []Option{WithFault("F-X-01", FaultOverloaded)}, text: "hashed", want: ai.ErrOverloaded, wantStatus: 529},
		{name: "server 500", opts: []Option{WithFault("F-X-01", FaultServer)}, text: "hashed", want: ErrServer, wantStatus: 500},
		{name: "malformed", opts: []Option{WithFault("F-X-01", FaultBadJSON)}, text: "hashed", want: ai.ErrBadOutput, wantStatus: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(fixtures(t), tt.opts...)
			_, err := f.Complete(context.Background(), userReq("F-X-01", tt.text, "r1"))
			var fe *Error
			if !errors.Is(err, tt.want) || !errors.As(err, &fe) || fe.Status != tt.wantStatus {
				t.Fatalf("err = %v, want %v status %d", err, tt.want, tt.wantStatus)
			}
			ch, err := New(fixtures(t), tt.opts...).Stream(context.Background(), userReq("F-X-01", tt.text, "r1"))
			if err != nil {
				t.Fatal(err)
			}
			ev := <-ch
			if !errors.Is(ev.Err, tt.want) {
				t.Fatalf("stream event err = %v, want %v", ev.Err, tt.want)
			}
			if _, open := <-ch; open {
				t.Fatal("stream not closed after Err")
			}
		})
	}
}

func TestStream(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		calls  int
		deltas string
		tools  int
	}{
		{name: "tool turn", text: "hashed", calls: 1, tools: 1},
		{name: "text turn", text: "hashed", calls: 2, deltas: "Proposed 1 entry for your review."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(fixtures(t))
			var ch <-chan ai.StreamEvent
			var err error
			for range tt.calls {
				if ch, err = f.Stream(context.Background(), userReq("F-X-01", tt.text, "r1")); err != nil {
					t.Fatal(err)
				}
			}
			var text strings.Builder
			tools, done := 0, 0
			for ev := range ch {
				text.WriteString(ev.TextDelta)
				if ev.ToolCallDelta != nil {
					tools++
				}
				if ev.Done != nil {
					done++
				}
			}
			if text.String() != tt.deltas || tools != tt.tools || done != 1 {
				t.Fatalf("deltas %q tools %d done %d", text.String(), tools, done)
			}
		})
	}
}

func TestDeterministic(t *testing.T) {
	tests := []struct {
		name string
		run  func(f *Fake) (any, error)
	}{
		{name: "complete", run: func(f *Fake) (any, error) { return f.Complete(context.Background(), userReq("F-X-01", "hashed", "r1")) }},
		{name: "stream", run: func(f *Fake) (any, error) {
			ch, err := f.Stream(context.Background(), userReq("F-X-01", "hashed", "r1"))
			var evs []ai.StreamEvent
			for ev := range ch {
				evs = append(evs, ev)
			}
			return evs, err
		}},
		{name: "embed", run: func(f *Fake) (any, error) {
			return f.Embed(context.Background(), ai.EmbedRequest{FeatureID: "F-X-01", Inputs: []string{"Synthetic Co", "other"}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var first []byte
			for i := range 100 {
				v, err := tt.run(New(fixtures(t)))
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					first = b
				} else if !bytes.Equal(b, first) {
					t.Fatalf("run %d differs", i)
				}
			}
		})
	}
}

func TestEmbed(t *testing.T) {
	f := New(fixtures(t), WithDim(64))
	tests := []struct {
		name string
		a, b string
		same bool
	}{
		{name: "case and space", a: "Synthetic Co", b: "  synthetic co ", same: true},
		{name: "different text", a: "Synthetic Co", b: "Other Ltd", same: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := f.Embed(context.Background(), ai.EmbedRequest{FeatureID: "F-X-01", Inputs: []string{tt.a, tt.b}})
			if err != nil {
				t.Fatal(err)
			}
			var dot, na float64
			for i := range resp.Vectors[0] {
				dot += float64(resp.Vectors[0][i]) * float64(resp.Vectors[1][i])
				na += float64(resp.Vectors[0][i]) * float64(resp.Vectors[0][i])
			}
			if len(resp.Vectors[0]) != 64 || math.Abs(na-1) > 1e-5 {
				t.Fatalf("dim %d norm² %f, want 64 and 1", len(resp.Vectors[0]), na)
			}
			if got := math.Abs(dot-1) < 1e-5; got != tt.same {
				t.Fatalf("cos = %f, same = %v", dot, tt.same)
			}
		})
	}
	if _, err := f.Embed(context.Background(), ai.EmbedRequest{Inputs: []string{"x"}}); !errors.Is(err, ai.ErrNoFeature) {
		t.Fatalf("Embed without feature err = %v", err)
	}
	if f.Mode() != ai.ModeMock {
		t.Fatalf("Mode = %q", f.Mode())
	}
}
