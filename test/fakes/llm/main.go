// Command llm is the fake language-model server for dev, CI and e2e (spec/ai/mocks.md sec. 5).
// It answers from the fixtures under testdata/ai (format: kernel/ai/fixture) and never makes an
// outbound call. Protocols live one per file; this file holds flags, the shared routes and the
// protocol-neutral fixture routing.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/fixture"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dir := flag.String("fixtures", "testdata/ai", "fixture root directory (testdata/ai)")
	models := flag.String("models", "drafting,fast,embed", "comma-separated logical model names listed by /v1/models")
	dump := flag.String("dump-bodies", "", "debug only: write request and response bodies of every call into this directory")
	speed := flag.Float64("speed", 1, "multiplier for fixture latency_ms and chunk_delay_ms; 0 zeroes all delays")
	health := flag.Bool("healthcheck", false, "probe /healthz on -addr, exit 0 when healthy (container healthcheck)")
	flag.Parse()

	if *health {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := healthcheck(ctx, *addr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := newServer(os.DirFS(*dir), splitList(*models))
	s.journal.dumpDir, s.speed = *dump, *speed
	if err := serve(ctx, *addr, s); err != nil {
		slog.Error("llm fake stopped", "err", err)
		os.Exit(1)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// serve runs the server on addr until ctx is done.
func serve(ctx context.Context, addr string, s *server) error {
	srv := &http.Server{Addr: addr, Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("llm fake listening", "addr", addr)
	select {
	case err := <-errc:
		return fmt.Errorf("listen %s: %w", addr, err)
	case <-ctx.Done():
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shut); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// healthcheck probes /healthz on the loopback address of the port in addr.
func healthcheck(ctx context.Context, addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("healthcheck: addr %q: %w", addr, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	return nil
}

// server answers model calls from fixtures. It holds no HTTP client: it has no way to reach
// a real model.
type server struct {
	fsys   fs.FS
	models []string

	mu    sync.Mutex
	calls map[string]int // fixture path + run id -> calls answered

	speed   float64 // delay multiplier; 0 zeroes latency_ms and chunk_delay_ms
	faults  *faultState
	journal *journal
}

func newServer(fsys fs.FS, models []string) *server {
	return &server{fsys: fsys, models: models, calls: map[string]int{}, speed: 1, faults: &faultState{}, journal: &journal{}}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /v1/models", s.listModels)
	mux.HandleFunc("POST /v1/messages", s.instrument("messages", decodeMessagesCall, writeMessagesError, s.messages))
	mux.HandleFunc("POST /v1/messages/count_tokens", s.countTokens)
	mux.HandleFunc("POST /v1/chat/completions", s.instrument("chat", decodeChatCall, writeOpenAIError, s.chatCompletions))
	mux.HandleFunc("POST /v1/responses", s.instrument("responses", decodeResponsesCall, writeOpenAIError, s.responses))
	mux.HandleFunc("POST /v1/embeddings", s.instrument("embeddings", decodeEmbeddingsCall, writeEmbeddingsError, s.embeddings))
	mux.HandleFunc("GET /_journal", s.journalList)
	mux.HandleFunc("POST /_reset", s.reset)
	return mux
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// modelsCreated is a fixed timestamp so /v1/models is byte-identical across runs.
const modelsCreated = "2026-01-01T00:00:00Z"

// listModels lists the logical model names in a shape both the Messages-style and the
// OpenAI-compatible clients accept.
func (s *server) listModels(w http.ResponseWriter, _ *http.Request) {
	type model struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Object      string `json:"object"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
		Created     int64  `json:"created"`
		OwnedBy     string `json:"owned_by"`
	}
	created, _ := time.Parse(time.RFC3339, modelsCreated)
	data := make([]model, 0, len(s.models))
	for _, m := range s.models {
		data = append(data, model{ID: m, Type: "model", Object: "model", DisplayName: m, CreatedAt: modelsCreated, Created: created.Unix(), OwnedBy: "fake"})
	}
	out := map[string]any{"object": "list", "data": data, "has_more": false}
	if len(data) > 0 {
		out["first_id"], out["last_id"] = data[0].ID, data[len(data)-1].ID
	}
	writeJSON(w, http.StatusOK, out)
}

// call is one model request reduced to what routing needs, whatever the protocol.
type call struct {
	feature  string
	runID    string
	system   string
	messages []ai.Message
	tools    []ai.ToolSpec
}

// apiError is a routing or request failure; each protocol renders it in its own error shape.
type apiError struct {
	status  int
	typ     string
	message string
	extra   map[string]any // e.g. feature, prompt_hash, nearest for no_fixture
}

// reply is one fixture turn ready to render.
type reply struct {
	id         string // deterministic message id
	fixture    string
	text       string
	toolCalls  []ai.ToolCall
	stopReason string
	usage      ai.Usage
}

var (
	systemFeatureRE = regexp.MustCompile(`^\s*feature:\s*(F-[A-Za-z0-9-]+)\s*$`)
	systemRunRE     = regexp.MustCompile(`(?m)^\s*run(?:_id)?:\s*(\S+)\s*$`)
)

// featureFromSystem reads a "feature: F-..." first line of the system prompt (the Paploo path,
// mocks.md sec. 9).
func featureFromSystem(system string) string {
	first, _, _ := strings.Cut(system, "\n")
	if m := systemFeatureRE.FindStringSubmatch(first); m != nil {
		return m[1]
	}
	return ""
}

// runFromSystem reads a "run: <id>" (or "run_id: <id>") line of the system prompt.
func runFromSystem(system string) string {
	if m := systemRunRE.FindStringSubmatch(system); m != nil {
		return m[1]
	}
	return ""
}

// lastUserText is the text of the last user message that has any text; a user message holding
// only tool results is skipped, so a fixture regex keeps matching across a tool loop.
func lastUserText(msgs []ai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		var b strings.Builder
		for _, c := range msgs[i].Content {
			if c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// route finds the fixture and turn for c: prompt hash first, then last_user_regex, then a
// fixture with neither. A miss is a 404 naming the computed hash and the nearest fixture.
func (s *server) route(c call) (fixture.Fixture, fixture.Turn, int, *apiError) {
	hash, err := fixture.PromptHash(fixture.Prompt{Feature: c.feature, System: c.system, Messages: c.messages, Tools: c.tools})
	if err != nil {
		return fixture.Fixture{}, fixture.Turn{}, 0, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: err.Error()}
	}
	miss := func(msg, nearest string) *apiError {
		return &apiError{status: http.StatusNotFound, typ: "no_fixture", message: msg,
			extra: map[string]any{"feature": c.feature, "prompt_hash": hash, "nearest": nearest}}
	}
	if c.feature == "" {
		return fixture.Fixture{}, fixture.Turn{}, 0, miss("no feature id: send X-Counsel-Feature, metadata.feature or a 'feature: F-...' first system line", "")
	}
	fxs, err := fixture.LoadFeature(s.fsys, c.feature)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fixture.Fixture{}, fixture.Turn{}, 0, &apiError{status: http.StatusInternalServerError, typ: "api_error", message: err.Error()}
	}
	fx, ok := pick(fxs, hash, lastUserText(c.messages))
	if !ok {
		nearest, best := "", -1
		for _, f := range fxs {
			if n := commonPrefix(f.Match.PromptHash, hash); n > best {
				nearest, best = f.Path, n
			}
		}
		return fixture.Fixture{}, fixture.Turn{}, 0, miss(fmt.Sprintf("no fixture for feature %s and prompt hash %s", c.feature, hash), nearest)
	}
	if fx.Fault != fixture.FaultNone {
		// The fault layer (faults.go) serves fault fixtures before routing; a fault fixture that
		// reaches here (its X-Fake-Fault-Once fault already fired) has no reply to give.
		return fixture.Fixture{}, fixture.Turn{}, 0, &apiError{status: http.StatusNotImplemented, typ: "fault_unsupported",
			message: fmt.Sprintf("fixture %s scripts fault %s, not served by this build", fx.Path, fx.Fault)}
	}
	if len(fx.Turns) == 0 {
		return fixture.Fixture{}, fixture.Turn{}, 0, miss(fmt.Sprintf("fixture %s has no turns", fx.Path), fx.Path)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.turn(fx.Path, c)
	t := fx.Turns[min(n, len(fx.Turns)-1)]
	if e := checkExpect(t, c); e != nil {
		return fixture.Fixture{}, fixture.Turn{}, 0, e // a refused call does not advance the run
	}
	if c.runID != "" {
		s.calls[fx.Path+"\x00"+c.runID] = n + 1
	}
	return fx, t, n, nil
}

func pick(fxs []fixture.Fixture, hash, lastUser string) (fixture.Fixture, bool) {
	for _, f := range fxs {
		if f.Match.PromptHash != "" && f.Match.PromptHash == hash {
			return f, true
		}
	}
	for _, f := range fxs {
		if f.Match.LastUserRegex != "" && f.Matches("", lastUser) {
			return f, true
		}
	}
	for _, f := range fxs {
		if f.Match.PromptHash == "" && f.Match.LastUserRegex == "" {
			return f, true
		}
	}
	return fixture.Fixture{}, false
}

// turn returns the 0-based turn index of this call; the caller holds s.mu. With a run id, turn
// N answers the N-th answered call of the run (in-memory sequence). Without one, the turn is the
// number of assistant messages already in the conversation, so a client that resends full
// history needs no run id.
func (s *server) turn(path string, c call) int {
	if c.runID == "" {
		n := 0
		for _, m := range c.messages {
			if m.Role == "assistant" {
				n++
			}
		}
		return n
	}
	return s.calls[path+"\x00"+c.runID]
}

// checkExpect asserts the request offered the turn's expect_tools and carries its
// expect_tool_results (tool results in the last message, named via their tool_use ids).
func checkExpect(t fixture.Turn, c call) *apiError {
	offered := make([]string, 0, len(c.tools))
	for _, tl := range c.tools {
		offered = append(offered, tl.Name)
	}
	var missing []string
	for _, want := range t.ExpectTools {
		if !slices.Contains(offered, want) {
			missing = append(missing, "tool "+want)
		}
	}
	if len(t.ExpectToolResults) > 0 {
		names := map[string]string{}
		for _, m := range c.messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" {
					names[b.ToolUseID] = b.ToolName
				}
			}
		}
		var got []string
		if len(c.messages) > 0 {
			for _, b := range c.messages[len(c.messages)-1].Content {
				if b.Type == "tool_result" {
					got = append(got, names[b.ToolUseID])
				}
			}
		}
		for _, want := range t.ExpectToolResults {
			if !slices.Contains(got, want) {
				missing = append(missing, "tool_result "+want)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &apiError{status: http.StatusBadRequest, typ: "expectation_failed",
		message: "request lacks what the fixture turn expects: " + strings.Join(missing, ", ")}
}

// estimateTokens is the deterministic usage estimate: ceil(len/4).
func estimateTokens(n int) int { return (n + 3) / 4 }

// promptChars counts the text a request sends, for the input token estimate.
func promptChars(c call) int {
	n := len(c.system)
	for _, m := range c.messages {
		for _, b := range m.Content {
			n += len(b.Text) + len(b.Input)
		}
	}
	for _, t := range c.tools {
		n += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	return n
}

// buildReply turns a fixture turn into a protocol-neutral reply with deterministic ids.
func buildReply(fx fixture.Fixture, t fixture.Turn, n int, c call) reply {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d", fx.Path, c.runID, n))
	r := reply{id: hex.EncodeToString(sum[:12]), fixture: fx.Path, text: t.Reply.Text, stopReason: t.Reply.StopReason}
	if r.text == "" && len(t.Reply.JSON) > 0 {
		r.text = string(t.Reply.JSON) // extraction output travels as text
	}
	outChars := len(r.text)
	for i, tc := range t.Reply.ToolCalls {
		id := tc.ID
		if id == "" {
			s := sha256.Sum256(fmt.Appendf(nil, "%s|%d|%d", fx.Path, n, i))
			id = "toolu_" + hex.EncodeToString(s[:8])
		}
		in := json.RawMessage(tc.Input)
		if len(in) == 0 {
			in = json.RawMessage(`{}`)
		}
		r.toolCalls = append(r.toolCalls, ai.ToolCall{ID: id, Name: tc.Name, Input: in})
		outChars += len(in)
	}
	if r.stopReason == "" {
		r.stopReason = "end_turn"
		if len(r.toolCalls) > 0 {
			r.stopReason = "tool_use"
		}
	}
	r.usage = ai.Usage{InputTokens: t.Reply.Usage.InputTokens, OutputTokens: t.Reply.Usage.OutputTokens, CacheReadTokens: t.Reply.Usage.CacheReadTokens}
	if r.usage.InputTokens == 0 {
		r.usage.InputTokens = estimateTokens(promptChars(c))
	}
	if r.usage.OutputTokens == 0 {
		r.usage.OutputTokens = estimateTokens(outChars)
	}
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("llm fake: write response", "err", err)
	}
}
