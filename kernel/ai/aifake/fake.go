// Package aifake is a deterministic, in-process ai.ModelClient and ai.Embedder for unit
// tests. It answers from YAML fixtures by feature id and prompt hash, never touches the
// network, reads no clock and starts no goroutines.
//
// Fixtures live in testdata/ai/<feature>/ (spec/ai/mocks.md sec. 4); New takes an fs.FS
// rooted at testdata/ai, so a request for F-TIM-01 reads <fsys>/f-tim-01/**/*.yaml.
package aifake

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/ai"
)

// rule: AI-R-001
var (
	_ ai.ModelClient = (*Fake)(nil)
	_ ai.Embedder    = (*Fake)(nil)
)

// ErrServer is the typed error for a scripted provider 500.
var ErrServer = errors.New("aifake: provider server error")

// Fault is a scripted provider failure.
type Fault string

// Faults a fixture or WithFault can script.
const (
	FaultRateLimit  Fault = "rate_limit"
	FaultOverloaded Fault = "overloaded"
	FaultServer     Fault = "server_error"
	FaultTimeout    Fault = "timeout"
	FaultBadJSON    Fault = "malformed"
)

// Error is a scripted fault. It unwraps to the matching ai sentinel (or ErrServer).
type Error struct {
	Fault      Fault
	Status     int // HTTP status the fault stands for; 0 for a timeout
	RetryAfter time.Duration
	err        error
}

func (e *Error) Error() string {
	return fmt.Sprintf("aifake: %s (status %d): %v", e.Fault, e.Status, e.err)
}

// Unwrap returns the sentinel the fault maps to.
func (e *Error) Unwrap() error { return e.err }

func faultError(f Fault) error {
	switch f {
	case FaultRateLimit:
		return &Error{Fault: f, Status: 429, RetryAfter: time.Second, err: ai.ErrRateLimited}
	case FaultOverloaded:
		return &Error{Fault: f, Status: 529, err: ai.ErrOverloaded}
	case FaultServer:
		return &Error{Fault: f, Status: 500, err: ErrServer}
	case FaultTimeout:
		return &Error{Fault: f, err: ai.ErrTimeout}
	case FaultBadJSON:
		return &Error{Fault: f, Status: 200, err: ai.ErrBadOutput}
	}
	return nil
}

// Option configures a Fake.
type Option func(*Fake)

// WithFault makes every call for featureID fail with f.
func WithFault(featureID string, f Fault) Option {
	return func(x *Fake) { x.faults[featureID] = f }
}

// WithDim sets the embedding dimension (default 1024).
func WithDim(n int) Option {
	return func(x *Fake) { x.dim = n }
}

// Fake answers model calls from fixtures.
type Fake struct {
	fsys   fs.FS
	dim    int
	faults map[string]Fault

	mu     sync.Mutex
	calls  map[string]int // fixture path + run id -> calls answered
	embeds []EmbedCall    // Embed journal (embed.go)
}

// New returns a Fake reading fixtures from fsys (rooted at testdata/ai).
func New(fsys fs.FS, opts ...Option) *Fake {
	f := &Fake{fsys: fsys, dim: 1024, faults: map[string]Fault{}, calls: map[string]int{}}
	for _, o := range opts {
		o(f)
	}
	return f
}

// Mode is always ai.ModeMock.
func (f *Fake) Mode() ai.Mode { return ai.ModeMock }

type fixture struct {
	Synthetic bool   `yaml:"synthetic"`
	Feature   string `yaml:"feature"`
	Match     struct {
		PromptHash    string `yaml:"prompt_hash"`
		LastUserRegex string `yaml:"last_user_regex"`
	} `yaml:"match"`
	Fault Fault  `yaml:"fault"`
	Turns []turn `yaml:"turns"`

	path string
}

type turn struct {
	Reply struct {
		Text      string `yaml:"text"`
		ToolCalls []struct {
			Name  string `yaml:"name"`
			Input any    `yaml:"input"`
		} `yaml:"tool_calls"`
		JSON       any    `yaml:"json"`
		StopReason string `yaml:"stop_reason"`
		Usage      struct {
			InputTokens  int `yaml:"input_tokens"`
			OutputTokens int `yaml:"output_tokens"`
		} `yaml:"usage"`
	} `yaml:"reply"`
}

// PromptHash is the hex sha256 of the canonical JSON of {feature, system, messages, tools}.
// Object keys are sorted and there is no insignificant whitespace.
func PromptHash(req ai.Request) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"feature":  req.FeatureID,
		"system":   req.System,
		"messages": req.Messages,
		"tools":    req.Tools,
	})
	if err != nil {
		return "", fmt.Errorf("aifake: hash request: %w", err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("aifake: hash request: %w", err)
	}
	canon, err := json.Marshal(v) // maps marshal with sorted keys
	if err != nil {
		return "", fmt.Errorf("aifake: hash request: %w", err)
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

func (f *Fake) fixtures(featureID string) ([]fixture, error) {
	dir := strings.ToLower(featureID)
	var out []fixture
	err := fs.WalkDir(f.fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() || path.Ext(p) != ".yaml" || strings.Contains(p, "/inputs/") {
			return nil
		}
		b, err := fs.ReadFile(f.fsys, p)
		if err != nil {
			return err
		}
		var fx fixture
		if err := yaml.Unmarshal(b, &fx); err != nil {
			return fmt.Errorf("aifake: fixture %s: %w", p, err)
		}
		fx.path = p
		out = append(out, fx)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

func lastUserText(req ai.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role != "user" {
			continue
		}
		var b strings.Builder
		for _, c := range m.Content {
			if c.Type == "text" {
				b.WriteString(c.Text)
			}
		}
		return b.String()
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

// lookup finds the fixture for req: prompt hash first, then last_user_regex.
func (f *Fake) lookup(req ai.Request) (fixture, error) {
	if err := req.Validate(); err != nil {
		return fixture{}, err
	}
	hash, err := PromptHash(req)
	if err != nil {
		return fixture{}, err
	}
	fxs, err := f.fixtures(req.FeatureID)
	if err != nil {
		return fixture{}, err
	}
	for _, fx := range fxs {
		if fx.Match.PromptHash == hash {
			return fx, nil
		}
	}
	last := lastUserText(req)
	for _, fx := range fxs {
		if fx.Match.LastUserRegex == "" {
			continue
		}
		re, err := regexp.Compile(fx.Match.LastUserRegex)
		if err != nil {
			return fixture{}, fmt.Errorf("aifake: fixture %s: %w", fx.path, err)
		}
		if re.MatchString(last) {
			return fx, nil
		}
	}
	nearest, best := "none", -1
	for _, fx := range fxs {
		if n := commonPrefix(fx.Match.PromptHash, hash); n > best {
			nearest, best = fx.path, n
		}
	}
	return fixture{}, fmt.Errorf("aifake: feature %s prompt_hash %s nearest %s: %w", req.FeatureID, hash, nearest, ai.ErrNoFixture)
}

// answer returns the response for the next call of req's run.
func (f *Fake) answer(req ai.Request) (ai.Response, error) {
	if fault, ok := f.faults[req.FeatureID]; ok {
		return ai.Response{}, faultError(fault)
	}
	fx, err := f.lookup(req)
	if err != nil {
		return ai.Response{}, err
	}
	f.mu.Lock()
	key := fx.path + "\x00" + req.Metadata["run_id"]
	n := f.calls[key]
	f.calls[key] = n + 1
	f.mu.Unlock()

	// A transient fault answers the first call of a run, so the retry path reaches the turns.
	switch fx.Fault {
	case FaultTimeout, FaultBadJSON:
		return ai.Response{}, faultError(fx.Fault)
	case FaultRateLimit, FaultOverloaded, FaultServer:
		if n == 0 {
			return ai.Response{}, faultError(fx.Fault)
		}
		n--
	}
	if len(fx.Turns) == 0 {
		return ai.Response{}, fmt.Errorf("aifake: fixture %s has no turns: %w", fx.path, ai.ErrNoFixture)
	}
	t := fx.Turns[min(n, len(fx.Turns)-1)]
	resp := ai.Response{Text: t.Reply.Text, StopReason: t.Reply.StopReason, Fixture: fx.path}
	for i, tc := range t.Reply.ToolCalls {
		in, err := json.Marshal(tc.Input)
		if err != nil {
			return ai.Response{}, fmt.Errorf("aifake: fixture %s tool input: %w", fx.path, err)
		}
		id := sha256.Sum256(fmt.Appendf(nil, "%s|%d|%d", fx.path, n, i))
		resp.ToolCalls = append(resp.ToolCalls, ai.ToolCall{ID: "toolu_" + hex.EncodeToString(id[:8]), Name: tc.Name, Input: in})
	}
	if t.Reply.JSON != nil {
		if resp.JSON, err = json.Marshal(t.Reply.JSON); err != nil {
			return ai.Response{}, fmt.Errorf("aifake: fixture %s json: %w", fx.path, err)
		}
	}
	if resp.StopReason == "" {
		resp.StopReason = "end_turn"
		if len(resp.ToolCalls) > 0 {
			resp.StopReason = "tool_use"
		}
	}
	resp.Usage.InputTokens = t.Reply.Usage.InputTokens
	resp.Usage.OutputTokens = t.Reply.Usage.OutputTokens
	if resp.Usage.OutputTokens == 0 {
		resp.Usage.OutputTokens = (len(resp.Text) + 3) / 4
	}
	return resp, nil
}

// Complete answers req from its fixture.
func (f *Fake) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	if err := ctx.Err(); err != nil {
		return ai.Response{}, err
	}
	return f.answer(req)
}

// Stream answers req as text deltas (one per word), then tool calls, then Done. The channel
// is filled and closed before Stream returns.
func (f *Fake) Stream(ctx context.Context, req ai.Request) (<-chan ai.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := f.answer(req)
	if err != nil {
		var fe *Error
		if !errors.As(err, &fe) {
			return nil, err
		}
		ch := make(chan ai.StreamEvent, 1)
		ch <- ai.StreamEvent{Err: err}
		close(ch)
		return ch, nil
	}
	var evs []ai.StreamEvent
	for _, w := range strings.SplitAfter(resp.Text, " ") {
		if w != "" {
			evs = append(evs, ai.StreamEvent{TextDelta: w})
		}
	}
	for i := range resp.ToolCalls {
		tc := resp.ToolCalls[i]
		evs = append(evs, ai.StreamEvent{ToolCallDelta: &tc})
	}
	done := resp
	evs = append(evs, ai.StreamEvent{Done: &done})
	ch := make(chan ai.StreamEvent, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// Vector is the deterministic unit vector for text: sha256(lowercase(trim(text))) expanded
// by counter-mode sha256 to dim values in [-1, 1], then normalised.
func Vector(text string, dim int) []float32 {
	seed := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(text))))
	v := make([]float64, dim)
	var block [sha256.Size]byte
	var ctr [8]byte
	for i := range dim {
		if i%8 == 0 {
			binary.BigEndian.PutUint64(ctr[:], uint64(i/8))
			block = sha256.Sum256(append(seed[:], ctr[:]...))
		}
		u := binary.BigEndian.Uint32(block[(i%8)*4:])
		v[i] = float64(u)/float64(math.MaxUint32)*2 - 1
	}
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dim)
	for i, x := range v {
		out[i] = float32(x / norm)
	}
	return out
}
