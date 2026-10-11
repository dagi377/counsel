package tools

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/httpclient"
)

// publicModelHosts are the public model provider API hosts. Tests reach a model only through
// aifake or test/fakes/llm, so none of these may appear in test inputs, and CI blocks them.
var publicModelHosts = []string{
	"api.anthropic.com",
	"api.openai.com",
	"generativelanguage.googleapis.com",
	"aiplatform.googleapis.com",
	"api.mistral.ai",
	"api.cohere.ai",
	"api.cohere.com",
	"openai.azure.com",
	"bedrock-runtime.amazonaws.com",
	"api.groq.com",
	"api.together.xyz",
	"api.x.ai",
}

// modelKeyEnvVars are the environment variables that carry a model provider key. A live key
// lives only in the vault (model.key_ref); none of these may be set in CI or named in tests.
var modelKeyEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"OPENAI_API_KEY",
	"AZURE_OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"COHERE_API_KEY",
	"GROQ_API_KEY",
	"TOGETHER_API_KEY",
	"XAI_API_KEY",
}

// liveModelRefs reports each public model host or model key env var named in src.
func liveModelRefs(src string) []string {
	var out []string
	for _, h := range publicModelHosts {
		if strings.Contains(src, h) {
			out = append(out, "model host "+h)
		}
	}
	for _, k := range modelKeyEnvVars {
		if strings.Contains(src, k) {
			out = append(out, "model key env "+k)
		}
	}
	return out
}

// rule: AI-R-001
func TestAI_R_001_LiveModelRefs(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"fake base url", "base_url: http://llm-fake:8080", nil},
		{"in-process fake", `model := aifake.New(os.DirFS("testdata/ai"))`, nil},
		{"vault key ref", "key_ref: vault://model/api_key", nil},
		{"real messages host", `BaseURL: "https://api.anthropic.com/v1"`, []string{"model host api.anthropic.com"}},
		{"real chat host", "base_url: https://api.openai.com", []string{"model host api.openai.com"}},
		{"azure host", "https://firm.openai.azure.com/openai", []string{"model host openai.azure.com"}},
		{"key env read", `os.Getenv("ANTHROPIC_API_KEY")`, []string{"model key env ANTHROPIC_API_KEY"}},
		{"key env in yaml", "env:\n  OPENAI_API_KEY: x", []string{"model key env OPENAI_API_KEY"}},
		{"host and key", "GEMINI_API_KEY=1 generativelanguage.googleapis.com", []string{"model host generativelanguage.googleapis.com", "model key env GEMINI_API_KEY"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := liveModelRefs(tt.src)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("liveModelRefs = %q, want %q", got, tt.want)
			}
		})
	}
}

// guardedFiles lists the repo files that must not name a real model: everything under any
// testdata/ directory and under test/, deploy/*.dev.yaml, and every Go test file except this
// one, so no test can build a client against a public model host or read a model key.
func guardedFiles(t *testing.T, root string) []string {
	t.Helper()
	self, err := filepath.Abs("no_live_model_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if p == self {
			return nil
		}
		dev, _ := filepath.Match("deploy/*.dev.yaml", rel)
		if dev || strings.HasPrefix(rel, "test/") || strings.HasPrefix(rel, "testdata/") ||
			strings.Contains(rel, "/testdata/") || strings.HasSuffix(rel, "_test.go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return files
}

// rule: AI-R-001
func TestAI_R_001_NoLiveModelInTestInputs(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	files := guardedFiles(t, root)
	var sawFixture, sawDev bool
	for _, p := range files {
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		sawFixture = sawFixture || strings.HasPrefix(rel, "testdata/ai/")
		sawDev = sawDev || rel == "deploy/counsel.dev.yaml"
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, ref := range liveModelRefs(string(raw)) {
			t.Errorf("%s names a real model: %s", rel, ref)
		}
	}
	// The guard must actually see the inputs it protects.
	if !sawFixture || !sawDev {
		t.Fatalf("guard missed inputs: testdata/ai seen %v, deploy/counsel.dev.yaml seen %v", sawFixture, sawDev)
	}
}

// ciEnv reports whether the run is CI or test (COUNSEL_ENV), where a live model is forbidden.
func ciEnv() (string, bool) {
	e := strings.ToLower(strings.TrimSpace(os.Getenv("COUNSEL_ENV")))
	return e, e == "ci" || e == "test"
}

// rule: AI-R-001
func TestAI_R_001_CIHasNoModelKey(t *testing.T) {
	env, ok := ciEnv()
	if !ok {
		t.Skip("COUNSEL_ENV is not ci or test")
	}
	for _, k := range modelKeyEnvVars {
		t.Run(k, func(t *testing.T) {
			if v, set := os.LookupEnv(k); set && v != "" {
				t.Fatalf("%s is set in COUNSEL_ENV=%s", k, env)
			}
		})
	}
}

// rule: AI-R-001
func TestAI_R_001_CIBlocksModelHosts(t *testing.T) {
	if _, ok := ciEnv(); !ok {
		t.Skip("COUNSEL_ENV is not ci or test")
	}
	for _, h := range publicModelHosts {
		t.Run(h, func(t *testing.T) {
			addrs, err := net.DefaultResolver.LookupHost(context.Background(), h)
			if err != nil {
				return // unresolvable is blocked
			}
			for _, a := range addrs {
				ip := net.ParseIP(a)
				if ip == nil || !(ip.IsUnspecified() || ip.IsLoopback()) {
					t.Fatalf("%s resolves to %s: egress to a public model host is not blocked", h, a)
				}
			}
		})
	}
}

// rule: AI-R-001
func TestAI_R_001_LiveClientRefusedInCI(t *testing.T) {
	key := func(context.Context, string) (string, error) { return "synthetic-key", nil }
	live := ai.Config{Mode: "live", BaseURL: "http://llm-fake:8080", KeyRef: "vault://model/api_key"}
	envs := []string{"ci", "test", "CI", " test "}
	if env, ok := ciEnv(); ok {
		envs = append(envs, env)
	}
	for _, env := range envs {
		t.Run(env, func(t *testing.T) {
			c, err := httpclient.FromConfig(context.Background(), live, env, fstest.MapFS{}, key)
			if !errors.Is(err, ai.ErrLiveModelForbidden) {
				t.Fatalf("FromConfig(live, %q) err = %v, want ErrLiveModelForbidden", env, err)
			}
			if c != nil {
				t.Fatalf("FromConfig(live, %q) built a client in mode %v", env, mode(c))
			}
		})
	}
}

func mode(c ai.ModelClient) ai.Mode {
	if m, ok := c.(interface{ Mode() ai.Mode }); ok {
		return m.Mode()
	}
	return ""
}
