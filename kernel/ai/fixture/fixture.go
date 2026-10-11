// Package fixture loads scripted model fixtures (testdata/ai/<feature>/<name>/<variant>.yaml)
// and computes the prompt hash that keys them. The in-process fake, the fake model server and
// the eval runner all read fixtures through this package, so there is one format.
package fixture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Sentinel errors (owned here).
var (
	ErrNotSynthetic = errors.New("fixture: missing synthetic: true")
	ErrInvalid      = errors.New("fixture: invalid")
)

// Variant names every fixture directory carries (bad_output is extra for extraction).
const (
	VariantOK        = "ok"
	VariantError429  = "error_429"
	VariantTimeout   = "timeout"
	VariantInjection = "injection"
	VariantBadOutput = "bad_output"
)

// RequiredVariants are the variants every feature fixture directory must have.
var RequiredVariants = []string{VariantOK, VariantError429, VariantTimeout, VariantInjection}

// Fault names a scripted failure.
type Fault string

// Faults a fixture may script. Empty means none.
const (
	FaultNone                Fault = ""
	FaultRateLimit           Fault = "rate_limit"
	FaultOverloaded          Fault = "overloaded"
	FaultServerError         Fault = "server_error"
	FaultDisconnectMidStream Fault = "disconnect_mid_stream"
	FaultMalformed           Fault = "malformed"
	FaultTimeout             Fault = "timeout"
)

func (f Fault) valid() bool {
	switch f {
	case FaultNone, FaultRateLimit, FaultOverloaded, FaultServerError,
		FaultDisconnectMidStream, FaultMalformed, FaultTimeout:
		return true
	}
	return false
}

// Fixture is one scripted answer for one feature.
type Fixture struct {
	Synthetic    bool                 `yaml:"synthetic"`
	Feature      string               `yaml:"feature"`
	Match        Match                `yaml:"match"`
	LatencyMS    int                  `yaml:"latency_ms"`
	ChunkDelayMS int                  `yaml:"chunk_delay_ms"`
	Fault        Fault                `yaml:"fault"`
	Turns        []Turn               `yaml:"turns"`
	Clusters     map[string][]string  `yaml:"clusters"` // embeddings: texts that sit near each other
	Vectors      map[string][]float32 `yaml:"vectors"`  // embeddings: pinned vectors by text

	// Path is the fs path the fixture was read from; Variant is its base name without ".yaml".
	Path    string `yaml:"-"`
	Variant string `yaml:"-"`
}

// Match selects a fixture for a request: prompt hash first, then a regex on the last user turn.
type Match struct {
	PromptHash    string `yaml:"prompt_hash"`
	LastUserRegex string `yaml:"last_user_regex"`

	re *regexp.Regexp
}

// Turn answers the N-th call of one run.
type Turn struct {
	ExpectTools       []string `yaml:"expect_tools"`
	ExpectToolResults []string `yaml:"expect_tool_results"`
	Reply             Reply    `yaml:"reply"`
}

// Reply is the scripted model output for one turn.
type Reply struct {
	Text       string     `yaml:"text"`
	ToolCalls  []ToolCall `yaml:"tool_calls"`
	JSON       RawJSON    `yaml:"json"` // extraction output
	StopReason string     `yaml:"stop_reason"`
	Usage      Usage      `yaml:"usage"`
}

// ToolCall is one scripted tool call. Input is the YAML value re-encoded as JSON.
type ToolCall struct {
	ID    string  `yaml:"id"`
	Name  string  `yaml:"name"`
	Input RawJSON `yaml:"input"`
}

// Usage is the scripted token usage for one turn.
type Usage struct {
	InputTokens     int `yaml:"input_tokens"`
	OutputTokens    int `yaml:"output_tokens"`
	CacheReadTokens int `yaml:"cache_read_tokens"`
}

// RawJSON holds a YAML value as JSON bytes (nil when absent).
type RawJSON json.RawMessage

// UnmarshalYAML decodes any YAML value and stores it as JSON.
func (r *RawJSON) UnmarshalYAML(n *yaml.Node) error {
	var v any
	if err := n.Decode(&v); err != nil {
		return fmt.Errorf("decode yaml value: %w", err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode json value: %w", err)
	}
	*r = b
	return nil
}

// Parse decodes one fixture file. name is used for errors and to set Path and Variant.
// Unknown keys are rejected so a typo never silently drops a field.
func Parse(name string, data []byte) (Fixture, error) {
	var f Fixture
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Fixture{}, fmt.Errorf("parse fixture %s: %w: %w", name, ErrInvalid, err)
	}
	f.Path = name
	f.Variant = strings.TrimSuffix(path.Base(name), ".yaml")
	if err := f.validate(); err != nil {
		return Fixture{}, fmt.Errorf("fixture %s: %w", name, err)
	}
	return f, nil
}

func (f *Fixture) validate() error {
	// rule: AI-R-004
	if !f.Synthetic {
		return ErrNotSynthetic
	}
	if f.Feature == "" {
		return fmt.Errorf("%w: no feature", ErrInvalid)
	}
	if !f.Fault.valid() {
		return fmt.Errorf("%w: unknown fault %q", ErrInvalid, f.Fault)
	}
	if len(f.Turns) == 0 && f.Fault == FaultNone && len(f.Clusters) == 0 && len(f.Vectors) == 0 {
		return fmt.Errorf("%w: no turns", ErrInvalid)
	}
	if f.Match.LastUserRegex != "" {
		re, err := regexp.Compile(f.Match.LastUserRegex)
		if err != nil {
			return fmt.Errorf("%w: last_user_regex: %w", ErrInvalid, err)
		}
		f.Match.re = re
	}
	for i, t := range f.Turns {
		for j, c := range t.Reply.ToolCalls {
			if c.Name == "" {
				return fmt.Errorf("%w: turn %d tool call %d has no name", ErrInvalid, i, j)
			}
		}
	}
	return nil
}

// Matches reports whether the fixture answers a request with this prompt hash and last user
// text: an equal prompt_hash, or else a last_user_regex hit. A fixture with neither matches
// any request for its feature.
func (f Fixture) Matches(promptHash, lastUser string) bool {
	if f.Match.PromptHash != "" && f.Match.PromptHash == promptHash {
		return true
	}
	if f.Match.re != nil {
		return f.Match.re.MatchString(lastUser)
	}
	return f.Match.PromptHash == ""
}

// Load reads and parses one fixture file from fsys.
func Load(fsys fs.FS, name string) (Fixture, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Fixture{}, fmt.Errorf("read fixture %s: %w", name, err)
	}
	return Parse(name, data)
}

// LoadDir reads every *.yaml directly under dir, keyed by variant. Subdirectories such as
// inputs/ are skipped.
func LoadDir(fsys fs.FS, dir string) (map[string]Fixture, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read fixture dir %s: %w", dir, err)
	}
	out := make(map[string]Fixture)
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".yaml" {
			continue
		}
		f, err := Load(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[f.Variant] = f
	}
	return out, nil
}

// FeatureDir is the directory under the fixture root that holds a feature's fixtures:
// the feature id in lower case ("F-ABC-01" -> "f-abc-01").
func FeatureDir(featureID string) string {
	return strings.ToLower(featureID)
}

// LoadFeature reads every fixture under <FeatureDir(featureID)>/<name>/ in fsys, sorted by
// path. Each fixture's feature field must equal featureID.
func LoadFeature(fsys fs.FS, featureID string) ([]Fixture, error) {
	root := FeatureDir(featureID)
	names, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("read feature dir %s: %w", root, err)
	}
	var out []Fixture
	for _, n := range names {
		if !n.IsDir() {
			continue
		}
		byVariant, err := LoadDir(fsys, path.Join(root, n.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range byVariant {
			if f.Feature != featureID {
				return nil, fmt.Errorf("fixture %s: %w: feature %q, want %q", f.Path, ErrInvalid, f.Feature, featureID)
			}
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
