package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// faultFS holds a happy fixture (F-FLT-01, with latency and chunk delay) and a fixture that
// scripts a rate limit (F-FLT-02).
func faultFS() fstest.MapFS {
	return fstest.MapFS{
		"f-flt-01/ok/ok.yaml": &fstest.MapFile{Data: []byte(`synthetic: true
feature: F-FLT-01
latency_ms: 60
chunk_delay_ms: 15
turns:
  - reply:
      text: "one two three four five"
`)},
		"f-flt-02/limited/rate.yaml": &fstest.MapFile{Data: []byte("synthetic: true\nfeature: F-FLT-02\nfault: rate_limit\n")},
	}
}

func hello(stream bool) map[string]any {
	return map[string]any{"model": "fast", "stream": stream, "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
}

// do sends body to the live test server and returns status, header, body and the read error.
func do(t *testing.T, ctx context.Context, url string, hdr map[string]string, body any) (int, http.Header, string, error) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(out), err
}

func TestAI_R_001_Faults(t *testing.T) {
	// rule: AI-R-001
	ts := httptest.NewServer(newServer(faultFS(), nil).handler())
	defer ts.Close()
	cases := []struct {
		name       string
		hdr        map[string]string
		stream     bool
		timeout    time.Duration
		wantStatus int
		wantType   string
		wantRetry  bool
		wantErr    bool // the transport or body read fails
		badJSON    bool
	}{
		{name: "header rate limit", hdr: map[string]string{"X-Fake-Fault": "rate_limit"}, wantStatus: 429, wantType: "rate_limit_error", wantRetry: true},
		{name: "fixture rate limit", hdr: map[string]string{"X-Counsel-Feature": "F-FLT-02"}, wantStatus: 429, wantType: "rate_limit_error", wantRetry: true},
		{name: "server error", hdr: map[string]string{"X-Fake-Fault": "server_error"}, wantStatus: 500, wantType: "api_error"},
		{name: "overloaded", hdr: map[string]string{"X-Fake-Fault": "overloaded"}, wantStatus: 529, wantType: "overloaded_error", wantRetry: true},
		{name: "malformed", hdr: map[string]string{"X-Fake-Fault": "malformed"}, wantStatus: 200, badJSON: true},
		{name: "disconnect mid-stream", hdr: map[string]string{"X-Fake-Fault": "disconnect_mid_stream"}, stream: true, wantStatus: 200, wantErr: true},
		{name: "disconnect plain", hdr: map[string]string{"X-Fake-Fault": "disconnect_mid_stream"}, wantStatus: 200, wantErr: true},
		{name: "timeout holds until the client deadline", hdr: map[string]string{"X-Fake-Fault": "timeout"}, timeout: 150 * time.Millisecond, wantErr: true},
		{name: "unknown fault", hdr: map[string]string{"X-Fake-Fault": "gremlins"}, wantStatus: 400, wantType: "invalid_request_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hdr := map[string]string{"X-Counsel-Feature": "F-FLT-01"}
			for k, v := range tc.hdr {
				hdr[k] = v
			}
			ctx := context.Background()
			if tc.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			start := time.Now()
			status, h, body, err := do(t, ctx, ts.URL, hdr, hello(tc.stream))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want a transport or read error, got %d %q", status, body)
				}
				if tc.timeout > 0 && time.Since(start) < tc.timeout {
					t.Fatalf("returned after %v, before the client deadline", time.Since(start))
				}
				if tc.stream && !strings.Contains(body, "message_start") {
					t.Fatalf("mid-stream disconnect sent no chunk first: %q", body)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.wantStatus {
				t.Fatalf("status %d, want %d: %s", status, tc.wantStatus, body)
			}
			if got := h.Get("Retry-After") != ""; got != tc.wantRetry {
				t.Fatalf("retry-after %q, want present=%v", h.Get("Retry-After"), tc.wantRetry)
			}
			var v map[string]any
			jerr := json.Unmarshal([]byte(body), &v)
			if tc.badJSON {
				if jerr == nil || body == "" {
					t.Fatalf("malformed fault sent valid JSON %q", body)
				}
				return
			}
			if jerr != nil {
				t.Fatalf("body %q: %v", body, jerr)
			}
			if tc.wantType != "" && dig(v, "error", "type") != tc.wantType {
				t.Fatalf("error = %v, want type %s", v, tc.wantType)
			}
		})
	}
}

func TestFaultOnce(t *testing.T) {
	ts := httptest.NewServer(newServer(faultFS(), nil).handler())
	defer ts.Close()
	hdr := map[string]string{"X-Counsel-Feature": "F-FLT-01", "X-Fake-Fault": "overloaded", "X-Fake-Fault-Once": "true", "X-Counsel-Run": "r1"}
	for i, want := range []int{529, 200, 200} {
		status, _, body, err := do(t, context.Background(), ts.URL, hdr, hello(false))
		if err != nil || status != want {
			t.Fatalf("call %d: status %d (%v), want %d: %s", i, status, err, want, body)
		}
	}
}

func TestLatencyAndChunkDelay(t *testing.T) {
	cases := []struct {
		name    string
		speed   float64
		stream  bool
		atLeast time.Duration
		under   time.Duration
	}{
		// rule: AI-R-001
		{name: "latency before first byte", speed: 1, atLeast: 60 * time.Millisecond},
		{name: "latency plus delay between 5 text chunks and 2 trailing events", speed: 1, stream: true, atLeast: 60*time.Millisecond + 8*15*time.Millisecond},
		{name: "speed 0 zeroes all delays", speed: 0, stream: true, under: 50 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(faultFS(), nil)
			s.speed = tc.speed
			ts := httptest.NewServer(s.handler())
			defer ts.Close()
			start := time.Now()
			status, _, body, err := do(t, context.Background(), ts.URL, map[string]string{"X-Counsel-Feature": "F-FLT-01"}, hello(tc.stream))
			took := time.Since(start)
			if err != nil || status != 200 {
				t.Fatalf("status %d (%v): %s", status, err, body)
			}
			if took < tc.atLeast || (tc.under > 0 && took >= tc.under) {
				t.Fatalf("took %v, want >= %v and < %v", took, tc.atLeast, tc.under)
			}
		})
	}
}

func TestAI_R_005_JournalAndReset(t *testing.T) {
	// rule: AI-R-005
	dump := t.TempDir()
	cases := []struct {
		name    string
		dumpDir string
	}{{"no bodies by default", ""}, {"bodies to files with -dump-bodies", dump}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(readmeFS(t), nil)
			s.journal.dumpDir = tc.dumpDir
			h := s.handler()
			hdr := map[string]string{"X-Counsel-Feature": "F-TST-01", "X-Counsel-Run": "j1"}
			if rec := post(t, h, "/v1/messages", hdr, loopTurn1(false)); rec.Code != 200 {
				t.Fatalf("turn 1: %d %s", rec.Code, rec.Body)
			}
			if rec := post(t, h, "/v1/messages", hdr, loopTurn2(true, "toolu_a")); rec.Code != 200 {
				t.Fatalf("turn 2: %d %s", rec.Code, rec.Body)
			}
			if rec := post(t, h, "/v1/messages", map[string]string{"X-Fake-Fault": "rate_limit", "X-Counsel-Feature": "F-TST-01"}, loopTurn1(false)); rec.Code != 429 {
				t.Fatalf("fault call: %d", rec.Code)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_journal", nil))
			raw := rec.Body.String()
			var j struct{ Calls []entry }
			if err := json.Unmarshal([]byte(raw), &j); err != nil || len(j.Calls) != 3 {
				t.Fatalf("journal %s (%v)", raw, err)
			}
			if strings.Contains(raw, "rebuild") || strings.Contains(raw, "Proposed") {
				t.Fatalf("journal leaks prompt or completion text: %s", raw)
			}
			outTurn2 := estimateTokens(len("Proposed 1 entry for your review."))
			want := []struct {
				status   int
				in, out  int
				fault    string
				hasUsage bool
			}{{200, 812, 64, "", true}, {200, 0, outTurn2, "", true}, {429, 0, 0, "rate_limit", false}}
			for i, c := range j.Calls {
				w := want[i]
				if c.Seq != i+1 || c.Protocol != "messages" || c.Feature != "F-TST-01" || len(c.Hash) != 64 || c.Status != w.status || c.Fault != w.fault {
					t.Fatalf("call %d = %+v", i, c)
				}
				if (c.Usage != nil) != w.hasUsage || (c.Usage != nil && (c.Usage.OutputTokens != w.out || (w.in > 0 && c.Usage.InputTokens != w.in) || c.Usage.InputTokens == 0)) {
					t.Fatalf("call %d usage = %+v, want in %d out %d", i, c.Usage, w.in, w.out)
				}
				if (len(c.BodyFiles) > 0) != (tc.dumpDir != "") {
					t.Fatalf("call %d body files %v with dump dir %q", i, c.BodyFiles, tc.dumpDir)
				}
				for _, f := range c.BodyFiles {
					if _, err := os.Stat(f); err != nil {
						t.Fatal(err)
					}
				}
			}

			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/_reset", nil))
			if rec.Code != http.StatusNoContent {
				t.Fatalf("reset status %d", rec.Code)
			}
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_journal", nil))
			if strings.TrimSpace(rec.Body.String()) != `{"calls":[]}` {
				t.Fatalf("journal after reset = %s", rec.Body)
			}
			// The run's sequence restarted: the first call of run j1 is turn 1 again.
			r := post(t, h, "/v1/messages", hdr, loopTurn1(false))
			if r.Code != 200 || !strings.Contains(r.Body.String(), `"tool_use"`) {
				t.Fatalf("after reset run j1 did not restart at turn 1: %d %s", r.Code, r.Body)
			}
		})
	}
}
