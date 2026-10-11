package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// answer is what a client takes from one model call, whatever the protocol.
type answer struct {
	text, tool, args, toolID string
	inTokens, outTokens      int
}

// protocol drives one wire protocol through the README tool loop (F-TST-01).
type protocol struct {
	name  string
	path  string
	turn1 func(stream bool) map[string]any
	turn2 func(stream bool, id, args string) map[string]any
	parse func(t *testing.T, body string, stream bool) answer
}

var chatToolSpec = map[string]any{"type": "function", "function": map[string]any{"name": "report_plan", "description": "plan", "parameters": map[string]any{"type": "object"}}}
var respTool = map[string]any{"type": "function", "name": "report_plan", "description": "plan", "parameters": map[string]any{"type": "object"}}

const loopUser = "please rebuild day 2026-10-09"

func protocols() []protocol {
	return []protocol{
		{name: "messages", path: "/v1/messages",
			turn1: loopTurn1,
			turn2: func(stream bool, id, _ string) map[string]any { return loopTurn2(stream, id) },
			parse: parseMessages},
		{name: "chat", path: "/v1/chat/completions",
			turn1: func(stream bool) map[string]any {
				return map[string]any{"model": "drafting", "stream": stream, "tools": []any{chatToolSpec},
					"messages": []any{map[string]any{"role": "user", "content": loopUser}}}
			},
			turn2: func(stream bool, id, args string) map[string]any {
				return map[string]any{"model": "drafting", "stream": stream, "tools": []any{chatToolSpec}, "messages": []any{
					map[string]any{"role": "user", "content": loopUser},
					map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "report_plan", "arguments": args}}}},
					map[string]any{"role": "tool", "tool_call_id": id, "content": "ok"},
				}}
			},
			parse: parseChat},
		{name: "responses", path: "/v1/responses",
			turn1: func(stream bool) map[string]any {
				return map[string]any{"model": "drafting", "stream": stream, "tools": []any{respTool}, "input": loopUser}
			},
			turn2: func(stream bool, id, args string) map[string]any {
				return map[string]any{"model": "drafting", "stream": stream, "tools": []any{respTool}, "input": []any{
					map[string]any{"role": "user", "content": loopUser},
					map[string]any{"type": "function_call", "call_id": id, "name": "report_plan", "arguments": args},
					map[string]any{"type": "function_call_output", "call_id": id, "output": "ok"},
				}}
			},
			parse: parseResponses},
	}
}

func parseMessages(t *testing.T, body string, stream bool) answer {
	t.Helper()
	var a answer
	if !stream {
		var r messagesResponse
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		for _, b := range r.Content {
			switch b.Type {
			case "text":
				a.text += b.Text
			case "tool_use":
				a.tool, a.args, a.toolID = b.Name, string(b.Input), b.ID
			}
		}
		a.inTokens, a.outTokens = r.Usage.InputTokens, r.Usage.OutputTokens
		return a
	}
	for _, ev := range parseSSE(t, body) {
		switch ev.name {
		case "message_start":
			a.inTokens = int(ev.data["message"].(map[string]any)["usage"].(map[string]any)["input_tokens"].(float64))
		case "content_block_start":
			if cb := ev.data["content_block"].(map[string]any); cb["type"] == "tool_use" {
				a.tool, a.toolID = cb["name"].(string), cb["id"].(string)
			}
		case "content_block_delta":
			d := ev.data["delta"].(map[string]any)
			if s, ok := d["text"].(string); ok {
				a.text += s
			}
			if s, ok := d["partial_json"].(string); ok {
				a.args += s
			}
		case "message_delta":
			a.outTokens = int(ev.data["usage"].(map[string]any)["output_tokens"].(float64))
		}
	}
	return a
}

func parseChat(t *testing.T, body string, stream bool) answer {
	t.Helper()
	var a answer
	take := func(r chatResponse) {
		if r.Usage != nil {
			a.inTokens, a.outTokens = r.Usage.PromptTokens, r.Usage.CompletionTokens
		}
		for _, c := range r.Choices {
			var content string
			var calls []chatToolCall
			if c.Message != nil {
				_ = json.Unmarshal(c.Message.Content, &content)
				calls = c.Message.ToolCalls
			}
			if c.Delta != nil {
				if c.Delta.Content != nil {
					content = *c.Delta.Content
				}
				calls = c.Delta.ToolCalls
			}
			a.text += content
			for _, tc := range calls {
				if tc.ID != "" {
					a.toolID = tc.ID
				}
				a.tool += tc.Function.Name
				a.args += tc.Function.Arguments
			}
		}
	}
	if !stream {
		var r chatResponse
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		take(r)
		return a
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	done := false
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		if line == "[DONE]" {
			done = true
			continue
		}
		if done {
			t.Fatalf("chunk after [DONE]: %s", line)
		}
		var r chatResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("chunk %q: %v", line, err)
		}
		if r.Object != "chat.completion.chunk" {
			t.Fatalf("chunk object %q", r.Object)
		}
		take(r)
	}
	if !done {
		t.Fatal("stream did not end with data: [DONE]")
	}
	return a
}

func parseResponses(t *testing.T, body string, stream bool) answer {
	t.Helper()
	full := body
	if stream {
		evs := parseSSE(t, body)
		var a answer
		for _, ev := range evs {
			switch ev.name {
			case "response.output_text.delta":
				a.text += ev.data["delta"].(string)
			case "response.function_call_arguments.delta":
				a.args += ev.data["delta"].(string)
			}
		}
		last := evs[len(evs)-1]
		if last.name != "response.completed" {
			t.Fatalf("last event %s", last.name)
		}
		b, _ := json.Marshal(last.data["response"])
		final := parseResponses(t, string(b), false)
		if final.text != a.text || final.args != a.args {
			t.Fatalf("deltas %+v disagree with completed response %+v", a, final)
		}
		return final
	}
	var r responsesResponse
	if err := json.Unmarshal([]byte(full), &r); err != nil {
		t.Fatal(err)
	}
	var a answer
	for _, o := range r.Output {
		switch o.Type {
		case "message":
			for _, p := range o.Content {
				a.text += p.Text
			}
		case "function_call":
			a.tool, a.args, a.toolID = o.Name, *o.Arguments, o.CallID
		}
	}
	a.inTokens, a.outTokens = r.Usage.InputTokens, r.Usage.OutputTokens
	return a
}

// TestAI_R_001_ProtocolConformance sends the README tool loop through every protocol, plain
// and streamed, and expects the same text, tool name, tool arguments and usage.
func TestAI_R_001_ProtocolConformance(t *testing.T) {
	// rule: AI-R-001
	hdr := map[string]string{"X-Counsel-Feature": "F-TST-01"}
	type result struct{ turn1, turn2 answer }
	got := map[string]result{}
	for _, p := range protocols() {
		for _, stream := range []bool{false, true} {
			name := p.name
			if stream {
				name += "/stream"
			}
			t.Run(name, func(t *testing.T) {
				h := newServer(readmeFS(t), nil).handler()
				rec := post(t, h, p.path, hdr, p.turn1(stream))
				if rec.Code != http.StatusOK {
					t.Fatalf("turn 1 status %d: %s", rec.Code, rec.Body)
				}
				a1 := p.parse(t, rec.Body.String(), stream)
				if a1.tool != "report_plan" || a1.text != "" || a1.toolID == "" {
					t.Fatalf("turn 1 = %+v", a1)
				}
				rec = post(t, h, p.path, hdr, p.turn2(stream, a1.toolID, a1.args))
				if rec.Code != http.StatusOK {
					t.Fatalf("turn 2 status %d: %s", rec.Code, rec.Body)
				}
				a2 := p.parse(t, rec.Body.String(), stream)
				if a2.text != "Proposed 1 entry for your review." || a2.tool != "" {
					t.Fatalf("turn 2 = %+v", a2)
				}
				a1.toolID, a2.toolID = "", ""
				got[name] = result{a1, a2}
			})
		}
	}
	want := got["messages"]
	if want.turn1.inTokens != 812 || want.turn1.outTokens != 64 {
		t.Fatalf("messages turn 1 usage = %+v, want fixture usage 812/64", want.turn1)
	}
	var args any
	if err := json.Unmarshal([]byte(want.turn1.args), &args); err != nil {
		t.Fatalf("tool arguments %q: %v", want.turn1.args, err)
	}
	for name, r := range got {
		if r.turn1 != want.turn1 {
			t.Errorf("%s turn 1 = %+v, want %+v", name, r.turn1, want.turn1)
		}
		if r.turn2.text != want.turn2.text || r.turn2.outTokens != want.turn2.outTokens {
			t.Errorf("%s turn 2 = %+v, want %+v", name, r.turn2, want.turn2)
		}
	}
}

func TestOpenAIRequestsMapToSamePrompt(t *testing.T) {
	// rule: AI-R-001
	// Each protocol's turn 2 must reduce to the same feature, messages and tools as the
	// Messages request, so prompt-hash fixtures route identically.
	hdr := map[string]string{"X-Counsel-Feature": "F-TST-01"}
	req := func(path string, body map[string]any) *http.Request {
		b, _ := json.Marshal(body)
		r, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(string(b)))
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	decode := func(t *testing.T, r *http.Request, v any) {
		t.Helper()
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	ps := protocols()
	var mq messagesRequest
	mr := req(ps[0].path, ps[0].turn2(false, "toolu_1", `{}`))
	decode(t, mr, &mq)
	base, err := mq.toCall(mr)
	if err != nil {
		t.Fatal(err)
	}
	var cq chatRequest
	cr := req(ps[1].path, ps[1].turn2(false, "toolu_1", `{}`))
	decode(t, cr, &cq)
	cc, err := cq.toCall(cr)
	if err != nil {
		t.Fatal(err)
	}
	var rq responsesRequest
	rr := req(ps[2].path, ps[2].turn2(false, "toolu_1", `{}`))
	decode(t, rr, &rq)
	rc, err := rq.toCall(rr)
	if err != nil {
		t.Fatal(err)
	}
	enc := func(c call) string {
		b, _ := json.Marshal(map[string]any{"f": c.feature, "s": c.system, "m": c.messages, "t": len(c.tools)})
		return string(b)
	}
	for name, c := range map[string]call{"chat": cc, "responses": rc} {
		if enc(c) != enc(base) {
			t.Errorf("%s call = %s\nwant %s", name, enc(c), enc(base))
		}
	}
}

func TestOpenAIErrorShape(t *testing.T) {
	cases := []struct {
		name, path string
		hdr        map[string]string
		body       any
		status     int
		typ        string
	}{
		{"chat no feature", "/v1/chat/completions", nil, map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}, http.StatusNotFound, "no_fixture"},
		{"chat bad role", "/v1/chat/completions", nil, map[string]any{"messages": []any{map[string]any{"role": "robot", "content": "hi"}}}, http.StatusBadRequest, "invalid_request_error"},
		{"responses no fixture", "/v1/responses", map[string]string{"X-Counsel-Feature": "F-NONE-01"}, map[string]any{"input": "hi"}, http.StatusNotFound, "no_fixture"},
		{"responses bad item", "/v1/responses", nil, map[string]any{"input": []any{map[string]any{"type": "image"}}}, http.StatusBadRequest, "invalid_request_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, newServer(readmeFS(t), nil).handler(), tc.path, tc.hdr, tc.body)
			var body struct {
				Error map[string]any `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.status || body.Error["type"] != tc.typ {
				t.Fatalf("got %d %v, want %d %s", rec.Code, body.Error, tc.status, tc.typ)
			}
		})
	}
}
