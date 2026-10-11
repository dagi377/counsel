package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// readmeFixtures returns the fixture examples in testdata/ai/README.md (yaml blocks that
// start with "synthetic:"), so the server is tested against the documented format.
func readmeFixtures(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/ai/README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	re := regexp.MustCompile("(?s)```yaml\n(.*?)```")
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		if strings.HasPrefix(m[1], "synthetic:") {
			out = append(out, m[1])
		}
	}
	if len(out) < 3 {
		t.Fatalf("README has %d fixture examples, want >= 3", len(out))
	}
	return out
}

// readmeFS puts each README example at <feature>/readme-<i>/ok.yaml.
func readmeFS(t *testing.T) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	for i, y := range readmeFixtures(t) {
		f, err := fixture.Parse(fmt.Sprintf("readme-%d.yaml", i), []byte(y))
		if err != nil {
			t.Fatalf("README example %d: %v", i, err)
		}
		fsys[fmt.Sprintf("%s/readme-%d/ok.yaml", fixture.FeatureDir(f.Feature), i)] = &fstest.MapFile{Data: []byte(y)}
	}
	return fsys
}

func TestReadmeExamplesParse(t *testing.T) {
	for i, y := range readmeFixtures(t) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if _, err := fixture.Parse("example.yaml", []byte(y)); err != nil {
				t.Fatalf("parse: %v", err)
			}
		})
	}
}

func post(t *testing.T, h http.Handler, path string, hdr map[string]string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var reportPlanTool = map[string]any{"name": "report_plan", "description": "plan", "input_schema": map[string]any{"type": "object"}}

// loopTurn1 and loopTurn2 are the two calls of the README tool loop (feature F-TST-01).
func loopTurn1(stream bool) map[string]any {
	return map[string]any{
		"model": "drafting", "max_tokens": 256, "stream": stream,
		"tools":    []any{reportPlanTool},
		"messages": []any{map[string]any{"role": "user", "content": "please rebuild day 2026-10-09"}},
	}
}

func loopTurn2(stream bool, toolUseID string) map[string]any {
	return map[string]any{
		"model": "drafting", "max_tokens": 256, "stream": stream,
		"tools": []any{reportPlanTool},
		"messages": []any{
			map[string]any{"role": "user", "content": "please rebuild day 2026-10-09"},
			map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": toolUseID, "name": "report_plan", "input": map[string]any{}}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": toolUseID, "content": "ok"}}},
		},
	}
}

func TestAI_R_001_MessagesToolLoop(t *testing.T) {
	// rule: AI-R-001
	h := newServer(readmeFS(t), nil).handler()
	hdr := map[string]string{"X-Counsel-Feature": "F-TST-01"}

	rec := post(t, h, "/v1/messages", hdr, loopTurn1(false))
	if rec.Code != http.StatusOK {
		t.Fatalf("turn 1 status %d: %s", rec.Code, rec.Body)
	}
	var r1 messagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	if r1.Type != "message" || r1.Role != "assistant" || *r1.StopReason != "tool_use" || len(r1.Content) != 1 {
		t.Fatalf("turn 1 = %+v", r1)
	}
	tu := r1.Content[0]
	if tu.Type != "tool_use" || tu.Name != "report_plan" || !strings.HasPrefix(tu.ID, "toolu_") {
		t.Fatalf("turn 1 block = %+v", tu)
	}
	if want := `{"steps":[{"id":"rebuild","title":"Rebuild 2026-10-09"}]}`; string(tu.Input) != want {
		t.Fatalf("tool input = %s, want %s", tu.Input, want)
	}
	if r1.Usage.InputTokens != 812 || r1.Usage.OutputTokens != 64 {
		t.Fatalf("usage = %+v, want fixture usage 812/64", r1.Usage)
	}

	rec = post(t, h, "/v1/messages", hdr, loopTurn2(false, tu.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("turn 2 status %d: %s", rec.Code, rec.Body)
	}
	var r2 messagesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}
	if *r2.StopReason != "end_turn" || len(r2.Content) != 1 || r2.Content[0].Text != "Proposed 1 entry for your review." {
		t.Fatalf("turn 2 = %+v", r2)
	}
	if want := estimateTokens(len("Proposed 1 entry for your review.")); r2.Usage.OutputTokens != want {
		t.Fatalf("estimated output tokens = %d, want %d", r2.Usage.OutputTokens, want)
	}
}

type sseEvent struct {
	name string
	data map[string]any
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &cur.data); err != nil {
				t.Fatalf("data %q: %v", line, err)
			}
		case line == "":
			if cur.name != "" {
				if cur.data["type"] != cur.name {
					t.Fatalf("event %s carries type %v", cur.name, cur.data["type"])
				}
				out = append(out, cur)
			}
			cur = sseEvent{}
		}
	}
	return out
}

func TestMessagesStream(t *testing.T) {
	cases := []struct {
		name       string
		body       map[string]any
		wantSeq    []string
		wantDelta  string // concatenated text or partial_json
		deltaKey   string
		wantStop   string
		wantOutTok float64
	}{
		{
			name: "tool_use as input_json_delta",
			body: loopTurn1(true),
			wantSeq: []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta",
				"content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"},
			wantDelta: `{"steps":[{"id":"rebuild","title":"Rebuild 2026-10-09"}]}`, deltaKey: "partial_json",
			wantStop: "tool_use", wantOutTok: 64,
		},
		{
			name: "text as text_delta",
			body: loopTurn2(true, "toolu_x"),
			wantSeq: []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta",
				"content_block_delta", "content_block_delta", "content_block_delta", "content_block_delta",
				"content_block_stop", "message_delta", "message_stop"},
			wantDelta: "Proposed 1 entry for your review.", deltaKey: "text",
			wantStop: "end_turn", wantOutTok: float64(estimateTokens(len("Proposed 1 entry for your review."))),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newServer(readmeFS(t), nil).handler()
			rec := post(t, h, "/v1/messages", map[string]string{"X-Counsel-Feature": "F-TST-01"}, tc.body)
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("status %d type %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
			}
			evs := parseSSE(t, rec.Body.String())
			var seq []string
			var delta strings.Builder
			for _, e := range evs {
				seq = append(seq, e.name)
				if e.name == "content_block_delta" {
					d := e.data["delta"].(map[string]any)
					delta.WriteString(d[tc.deltaKey].(string))
				}
			}
			if strings.Join(seq, ",") != strings.Join(tc.wantSeq, ",") {
				t.Fatalf("events = %v, want %v", seq, tc.wantSeq)
			}
			if delta.String() != tc.wantDelta {
				t.Fatalf("deltas = %q, want %q", delta.String(), tc.wantDelta)
			}
			md := evs[len(evs)-2].data
			if got := md["delta"].(map[string]any)["stop_reason"]; got != tc.wantStop {
				t.Fatalf("stop_reason = %v, want %s", got, tc.wantStop)
			}
			if got := md["usage"].(map[string]any)["output_tokens"]; got != tc.wantOutTok {
				t.Fatalf("output_tokens = %v, want %v", got, tc.wantOutTok)
			}
		})
	}
}

func TestMessagesRouting(t *testing.T) {
	hello := []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "hello"}}}}
	hash, err := fixture.PromptHash(fixture.Prompt{Feature: "F-RT-01", Messages: hello})
	if err != nil {
		t.Fatal(err)
	}
	fx := func(match, text string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("synthetic: true\nfeature: F-RT-01\nmatch: " + match + "\nturns:\n  - reply: {text: \"" + text + "\"}\n")}
	}
	fsys := fstest.MapFS{
		"f-rt-01/by-hash/ok.yaml":  fx("{prompt_hash: "+hash+"}", "by hash"),
		"f-rt-01/by-regex/ok.yaml": fx("{last_user_regex: \"^hel+o\"}", "by regex"),
	}
	msg := func(text string) []any { return []any{map[string]any{"role": "user", "content": text}} }
	cases := []struct {
		name     string
		hdr      map[string]string
		body     map[string]any
		wantCode int
		wantText string
		wantErr  string
	}{
		{"header feature and hash", map[string]string{"X-Counsel-Feature": "F-RT-01"}, map[string]any{"messages": msg("hello")}, 200, "by hash", ""},
		{"metadata feature and hash", nil, map[string]any{"metadata": map[string]any{"feature": "F-RT-01"}, "messages": msg("hello")}, 200, "by hash", ""},
		{"system feature line, regex fallback", nil, map[string]any{"system": "feature: F-RT-01\nrun: r1\nYou help.", "messages": msg("hello")}, 200, "by regex", ""},
		{"system as text blocks", nil, map[string]any{"system": []any{map[string]any{"type": "text", "text": "feature: F-RT-01"}}, "messages": msg("helllo there")}, 200, "by regex", ""},
		{"regex fallback on stale hash", map[string]string{"X-Counsel-Feature": "F-RT-01"}, map[string]any{"messages": msg("hello again")}, 200, "by regex", ""},
		{"no match", map[string]string{"X-Counsel-Feature": "F-RT-01"}, map[string]any{"messages": msg("goodbye")}, 404, "", "no_fixture"},
		{"unknown feature", map[string]string{"X-Counsel-Feature": "F-NOPE-01"}, map[string]any{"messages": msg("hello")}, 404, "", "no_fixture"},
		{"no feature", nil, map[string]any{"messages": msg("hello")}, 404, "", "no_fixture"},
		{"bad body", map[string]string{"X-Counsel-Feature": "F-RT-01"}, map[string]any{"messages": "x"}, 400, "", "invalid_request_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, newServer(fsys, nil).handler(), "/v1/messages", tc.hdr, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.wantCode, rec.Body)
			}
			if tc.wantErr != "" {
				var e struct {
					Type  string         `json:"type"`
					Error map[string]any `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
					t.Fatal(err)
				}
				if e.Type != "error" || e.Error["type"] != tc.wantErr {
					t.Fatalf("error = %+v, want type %s", e, tc.wantErr)
				}
				if tc.wantErr == "no_fixture" {
					if h, _ := e.Error["prompt_hash"].(string); len(h) != 64 {
						t.Fatalf("no_fixture error does not name the prompt hash: %+v", e.Error)
					}
				}
				return
			}
			var r messagesResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			if len(r.Content) != 1 || r.Content[0].Text != tc.wantText {
				t.Fatalf("content = %+v, want %q", r.Content, tc.wantText)
			}
		})
	}
	// The no-match error names the exact hash a fixture author must paste.
	rec := post(t, newServer(fsys, nil).handler(), "/v1/messages", map[string]string{"X-Counsel-Feature": "F-RT-01"}, map[string]any{"messages": msg("goodbye")})
	bye, _ := fixture.PromptHash(fixture.Prompt{Feature: "F-RT-01", Messages: []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: "goodbye"}}}}})
	if !strings.Contains(rec.Body.String(), bye) {
		t.Fatalf("404 body %s lacks hash %s", rec.Body, bye)
	}
}

func TestMessagesTurnSequence(t *testing.T) {
	cases := []struct {
		name     string
		calls    []map[string]any
		hdr      map[string]string
		wantLast int
		wantErr  string
	}{
		{"run id advances turns", []map[string]any{loopTurn1(false), loopTurn2(false, "toolu_a")}, map[string]string{"X-Counsel-Run": "r1"}, 200, ""},
		{"missing tool result is refused", []map[string]any{{
			"tools": []any{reportPlanTool},
			"messages": []any{
				map[string]any{"role": "user", "content": "rebuild day"},
				map[string]any{"role": "assistant", "content": "thinking"},
				map[string]any{"role": "user", "content": "rebuild day please"},
			},
		}}, nil, 400, "expectation_failed"},
		{"tool not offered is refused", []map[string]any{{"messages": []any{map[string]any{"role": "user", "content": "rebuild day"}}}}, nil, 400, "expectation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newServer(readmeFS(t), nil).handler()
			hdr := map[string]string{"X-Counsel-Feature": "F-TST-01"}
			for k, v := range tc.hdr {
				hdr[k] = v
			}
			var rec *httptest.ResponseRecorder
			for _, c := range tc.calls {
				rec = post(t, h, "/v1/messages", hdr, c)
			}
			if rec.Code != tc.wantLast {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.wantLast, rec.Body)
			}
			if tc.wantErr != "" && !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Fatalf("body %s lacks %s", rec.Body, tc.wantErr)
			}
		})
	}
}

func TestFaultFixtureRefused(t *testing.T) {
	// The README fault example (F-TST-01, fault: timeout) is not served as a happy path.
	fsys := fstest.MapFS{"f-tst-03/slow/timeout.yaml": &fstest.MapFile{Data: []byte("synthetic: true\nfeature: F-TST-03\nfault: timeout\n")}}
	rec := post(t, newServer(fsys, nil).handler(), "/v1/messages", map[string]string{"X-Counsel-Feature": "F-TST-03"},
		map[string]any{"messages": []any{map[string]any{"role": "user", "content": "x"}}})
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "fault_unsupported") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestHealthModelsCountTokens(t *testing.T) {
	h := newServer(fstest.MapFS{}, []string{"drafting", "fast"}).handler()
	cases := []struct {
		name, method, path string
		body               any
		want               string
	}{
		{"healthz", http.MethodGet, "/healthz", nil, "ok\n"},
		{"models", http.MethodGet, "/v1/models", nil, `"id":"drafting"`},
		{"count tokens", http.MethodPost, "/v1/messages/count_tokens", map[string]any{"messages": []any{map[string]any{"role": "user", "content": "12345678"}}}, `{"input_tokens":2}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec *httptest.ResponseRecorder
			if tc.method == http.MethodPost {
				rec = post(t, h, tc.path, nil, tc.body)
			} else {
				rec = httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			}
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("status %d body %s, want %s", rec.Code, rec.Body, tc.want)
			}
		})
	}
}

func TestHealthcheckFlag(t *testing.T) {
	srv := httptest.NewServer(newServer(fstest.MapFS{}, nil).handler())
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":"):]
	cases := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"healthy", port, false},
		{"bad addr", "nonsense", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := healthcheck(context.Background(), tc.addr); (err != nil) != tc.wantErr {
				t.Fatalf("healthcheck(%q) = %v, wantErr %v", tc.addr, err, tc.wantErr)
			}
		})
	}
}

func TestChunkJSONKeepsRunes(t *testing.T) {
	cases := []string{`{}`, `{"a":"Ünïcödé text that is long"}`, strings.Repeat("x", 40)}
	for _, in := range cases {
		parts := chunkJSON(in, jsonChunk)
		if strings.Join(parts, "") != in {
			t.Fatalf("chunks of %q do not rejoin", in)
		}
		for _, p := range parts {
			if !json.Valid([]byte(`"`+strings.ReplaceAll(p, `"`, `\"`)+`"`)) || len(p) > jsonChunk+3 {
				t.Fatalf("bad chunk %q", p)
			}
		}
	}
}
