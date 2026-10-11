package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Request journal (spec/ai/mocks.md sec. 5): GET /_journal lists every model call with its
// feature id, prompt hash, protocol, status and usage. It holds no prompt or completion text
// (AI-R-005); bodies go to files only when -dump-bodies names a directory.

// usage is the token usage a response reported.
type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// entry is one journalled call.
type entry struct {
	Seq       int      `json:"seq"`
	Protocol  string   `json:"protocol"`
	Feature   string   `json:"feature"`
	Hash      string   `json:"prompt_hash"`
	Status    int      `json:"status"`
	Fault     string   `json:"fault,omitempty"`
	LatencyMS int64    `json:"latency_ms"`
	Usage     *usage   `json:"usage,omitempty"`
	BodyFiles []string `json:"body_files,omitempty"` // only with -dump-bodies
}

type journal struct {
	mu      sync.Mutex
	entries []entry
	seq     int
	dumpDir string // empty: bodies are never kept
}

// add records ent; with a dump directory it writes the request and response bodies there.
// rule: AI-R-005
func (j *journal) add(ent entry, reqBody, respBody []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	ent.Seq = j.seq
	if j.dumpDir != "" {
		for _, f := range []struct {
			suffix string
			b      []byte
		}{{"request.json", reqBody}, {"response.txt", respBody}} {
			p := filepath.Join(j.dumpDir, fmt.Sprintf("%06d-%s", ent.Seq, f.suffix))
			if err := os.WriteFile(p, f.b, 0o600); err != nil {
				slog.Warn("llm fake: dump body", "path", p, "err", err)
				continue
			}
			ent.BodyFiles = append(ent.BodyFiles, p)
		}
	}
	j.entries = append(j.entries, ent)
}

func (j *journal) list() []entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]entry{}, j.entries...)
}

func (j *journal) reset() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries, j.seq = nil, 0
}

// journalList answers GET /_journal.
func (s *server) journalList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"calls": s.journal.list()})
}

// reset answers POST /_reset: it clears the journal, the per-run turn sequences and the
// fired X-Fake-Fault-Once faults, so each test starts clean.
func (s *server) reset(w http.ResponseWriter, _ *http.Request) {
	s.journal.reset()
	s.faults.reset()
	s.mu.Lock()
	s.calls = map[string]int{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// usageOf reads the token usage a response body reported, whatever the protocol: a JSON
// body's "usage", or the usage objects of an SSE stream (the latest value of each count wins).
func usageOf(body []byte) *usage {
	var u usage
	found := false
	take := func(doc []byte) {
		var m map[string]any
		if json.Unmarshal(doc, &m) != nil {
			return
		}
		for _, cand := range []any{m["usage"], dig(m, "message", "usage"), dig(m, "response", "usage")} {
			um, ok := cand.(map[string]any)
			if !ok {
				continue
			}
			for _, k := range []string{"input_tokens", "prompt_tokens"} {
				if v, ok := um[k].(float64); ok {
					u.InputTokens, found = int(v), true
				}
			}
			for _, k := range []string{"output_tokens", "completion_tokens"} {
				if v, ok := um[k].(float64); ok {
					u.OutputTokens, found = int(v), true
				}
			}
		}
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		take(trimmed)
	} else {
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(make([]byte, 0, 64<<10), maxBody)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				take([]byte(d))
			}
		}
	}
	if !found {
		return nil
	}
	return &u
}

func dig(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}
