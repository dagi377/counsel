package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

// roundTrip answers in-process, so no test opens a socket.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(status int, body string, hdr http.Header) roundTrip {
	return func(r *http.Request) (*http.Response, error) {
		if hdr == nil {
			hdr = http.Header{}
		}
		return &http.Response{StatusCode: status, Header: hdr, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
}

func client(rt roundTrip, key string) *Client {
	return New(Options{BaseURL: "http://llm-fake:8080", Key: key, Models: map[string]string{"fast": "provider-fast"}, HTTP: &http.Client{Transport: rt}})
}

var req = ai.Request{FeatureID: "F-X-01", Kind: ai.KindToolUse, Model: "fast", Metadata: map[string]string{"run_id": "r1"},
	Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hi"}}}}}

// rule: AI-R-001
func TestAI_R_001_FromConfig(t *testing.T) {
	key := func(_ context.Context, ref string) (string, error) {
		if ref == "vault://model/api_key" {
			return "sk-test", nil
		}
		return "", errors.New("no such ref")
	}
	empty := func(context.Context, string) (string, error) { return "", nil }
	tests := []struct {
		name     string
		cfg      ai.Config
		env      string
		key      KeyFunc
		wantMode ai.Mode
		fake     bool
		wantErr  error
	}{
		{"default mock is in-process", ai.Config{}, "dev", nil, ai.ModeMock, true, nil},
		{"mock with base url is http", ai.Config{Mode: "mock", BaseURL: "http://llm-fake:8080"}, "ci", nil, ai.ModeMock, false, nil},
		{"live in prod", ai.Config{Mode: "live", BaseURL: "https://m", KeyRef: "vault://model/api_key"}, "prod", key, ai.ModeLive, false, nil},
		{"live in ci refused", ai.Config{Mode: "live", BaseURL: "https://m", KeyRef: "vault://model/api_key"}, "ci", key, "", false, ai.ErrLiveModelForbidden},
		{"live in test refused", ai.Config{Mode: "live", BaseURL: "https://m", KeyRef: "vault://model/api_key"}, "test", key, "", false, ai.ErrLiveModelForbidden},
		{"live key ref unresolved", ai.Config{Mode: "live", BaseURL: "https://m", KeyRef: "vault://other"}, "prod", key, "", false, ai.ErrLiveModelForbidden},
		{"live empty key", ai.Config{Mode: "live", BaseURL: "https://m", KeyRef: "vault://model/api_key"}, "prod", empty, "", false, ErrNoKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := FromConfig(context.Background(), tt.cfg, tt.env, fstest.MapFS{}, tt.key)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if err != nil && strings.Contains(err.Error(), "sk-test") {
					t.Fatalf("error leaks key: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FromConfig: %v", err)
			}
			if c.Mode() != tt.wantMode {
				t.Fatalf("mode = %q, want %q", c.Mode(), tt.wantMode)
			}
			if _, ok := c.(*aifake.Fake); ok != tt.fake {
				t.Fatalf("in-process fake = %v, want %v", ok, tt.fake)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	ok := `{"content":[{"type":"text","text":"plan"},{"type":"tool_use","id":"t1","name":"report_plan","input":{"a":1}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":7}}`
	tests := []struct {
		name    string
		rt      roundTrip
		key     string
		wantErr error
	}{
		{"ok", reply(200, ok, nil), "", nil},
		{"rate limited", reply(429, `{"error":{"type":"rate_limit_error"}}`, http.Header{"Retry-After": {"1"}}), "", ai.ErrRateLimited},
		{"overloaded", reply(529, `{"error":{"type":"overloaded_error"}}`, nil), "", ai.ErrOverloaded},
		{"no fixture", reply(404, `{"error":{"type":"no_fixture"}}`, nil), "", ai.ErrNoFixture},
		{"other status", reply(500, `{}`, nil), "", ErrStatus},
		{"bad body", reply(200, `{`, nil), "", ai.ErrBadOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen *http.Request
			var body map[string]any
			rt := func(r *http.Request) (*http.Response, error) {
				seen = r
				_ = json.NewDecoder(r.Body).Decode(&body)
				return tt.rt(r)
			}
			got, err := client(rt, tt.key).Complete(context.Background(), req)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if seen.URL.Path != "/v1/messages" || seen.Header.Get("X-Counsel-Feature") != "F-X-01" || body["model"] != "provider-fast" {
				t.Fatalf("request = %s %v model %v", seen.URL.Path, seen.Header, body["model"])
			}
			if tt.wantErr != nil {
				return
			}
			if got.Text != "plan" || len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "report_plan" || got.StopReason != "tool_use" || got.Usage.OutputTokens != 7 {
				t.Fatalf("response = %+v", got)
			}
		})
	}
}

func TestCompleteNeedsFeature(t *testing.T) {
	_, err := client(reply(200, `{}`, nil), "").Complete(context.Background(), ai.Request{})
	if !errors.Is(err, ai.ErrNoFeature) {
		t.Fatalf("err = %v", err)
	}
}

func TestKeyHeaderOnlyWhenSet(t *testing.T) {
	tests := []struct{ name, key string }{{"mock", ""}, {"live", "sk-live"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			rt := func(r *http.Request) (*http.Response, error) {
				got = r.Header.Get("x-api-key")
				return reply(200, `{"content":[],"stop_reason":"end_turn"}`, nil)(r)
			}
			if _, err := client(rt, tt.key).Complete(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if got != tt.key {
				t.Fatalf("x-api-key = %q, want %q", got, tt.key)
			}
		})
	}
}

func TestStream(t *testing.T) {
	ev := func(v string) string { return "event: x\ndata: " + v + "\n\n" }
	full := ev(`{"type":"message_start","message":{"usage":{"input_tokens":3}}}`) +
		ev(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		ev(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`) +
		ev(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`) +
		ev(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"report_plan","input":{}}}`) +
		ev(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`) +
		ev(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"1}"}}`) +
		ev(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`) +
		ev(`{"type":"message_stop"}`)
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"complete", full, false},
		{"truncated", full[:strings.Index(full, "message_delta")-20], true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := client(reply(200, tt.body, nil), "").Stream(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			var text string
			var done *ai.Response
			var serr error
			for e := range ch {
				text += e.TextDelta
				if e.Done != nil {
					done = e.Done
				}
				if e.Err != nil {
					serr = e.Err
				}
			}
			if tt.wantErr {
				if serr == nil || done != nil {
					t.Fatalf("want error event, got done=%v err=%v", done, serr)
				}
				return
			}
			if serr != nil || done == nil {
				t.Fatalf("done=%v err=%v", done, serr)
			}
			if text != "hello" || done.Text != "hello" || len(done.ToolCalls) != 1 || string(done.ToolCalls[0].Input) != `{"a":1}` ||
				done.StopReason != "tool_use" || done.Usage.InputTokens != 3 || done.Usage.OutputTokens != 9 {
				t.Fatalf("done = %+v", done)
			}
		})
	}
}

// rule: AI-R-001
func TestAI_R_001_DeployConfigModes(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"counsel.dev.yaml", "mock"},
		{"counsel.prod.yaml", "live"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Model ai.Config `yaml:"model"`
			}
			if err := yaml.Unmarshal(b, &doc); err != nil {
				t.Fatalf("parse %s: %v", tt.file, err)
			}
			if doc.Model.Mode != tt.want {
				t.Fatalf("%s model.mode = %q, want %q", tt.file, doc.Model.Mode, tt.want)
			}
			if _, err := doc.Model.Check("ci"); doc.Model.Mode == "live" && !errors.Is(err, ai.ErrLiveModelForbidden) {
				t.Fatalf("%s in ci: err = %v, want live_model_forbidden", tt.file, err)
			}
		})
	}
}
