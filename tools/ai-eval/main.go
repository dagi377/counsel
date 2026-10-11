// Command ai-eval runs the AI eval cases in testdata/ai/evals against the in-process fake
// model (aifake) and reports pass/fail per feature. Every injection fixture also runs as a
// red-team case. --live is opt-in and refused in CI (spec/ai/mocks.md sec. 7).
//
// Run from the repo root: go run ./tools/ai-eval [--live] [--feature F-XXX-01]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.DirFS("."), os.Stdout, os.Stderr))
}

// liveRefused reports why a live run is not allowed here, or "" when it is.
//
// rule: AI-R-002
func liveRefused(getenv func(string) string) string {
	switch getenv("COUNSEL_ENV") {
	case "test", "ci":
		return "COUNSEL_ENV=" + getenv("COUNSEL_ENV")
	}
	if getenv("CI") != "" {
		return "CI is set"
	}
	return ""
}

func run(ctx context.Context, args []string, getenv func(string) string, repo fs.FS, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("ai-eval", flag.ContinueOnError)
	fl.SetOutput(stderr)
	live := fl.Bool("live", false, "run against a live model (opt-in; refused in CI)")
	only := fl.String("feature", "", "run only this feature id")
	fixtures := fl.String("fixtures", "testdata/ai", "fixture root, relative to the repo root")
	featuresPath := fl.String("features", "spec/ai/features.json", "features.json, relative to the repo root")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *live {
		if why := liveRefused(getenv); why != "" {
			fmt.Fprintf(stderr, "ai-eval: --live refused (%s): %v\n", why, ai.ErrLiveModelForbidden)
			return 2
		}
		// No live ModelClient is wired into this tool yet; a live run never falls back to a
		// real provider by accident.
		fmt.Fprintln(stderr, "ai-eval: --live needs a live model client, which is not configured here")
		return 2
	}

	features, err := loadFeatures(repo, *featuresPath)
	if err != nil {
		fmt.Fprintf(stderr, "ai-eval: %v\n", err)
		return 2
	}
	fsys, err := fs.Sub(repo, *fixtures)
	if err != nil {
		fmt.Fprintf(stderr, "ai-eval: %v\n", err)
		return 2
	}
	evals, err := LoadEvals(fsys, features)
	if err != nil {
		fmt.Fprintf(stderr, "ai-eval: %v\n", err)
		return 2
	}
	newFake := func(f fs.FS) ai.ModelClient { return aifake.New(f) }
	r := &Runner{NewClient: newFake, Fixtures: fsys, Features: features}
	failed, total := 0, 0
	for _, e := range evals {
		if *only != "" && e.Feature != *only {
			continue
		}
		pass := 0
		for _, c := range e.Cases {
			res := r.Run(ctx, e.Feature, c)
			total++
			if res.Pass() {
				pass++
				continue
			}
			for _, f := range res.Fails {
				fmt.Fprintf(stdout, "  FAIL %s %s: %s\n", e.Feature, res.Case, f)
			}
		}
		status := "PASS"
		if pass != len(e.Cases) {
			status = "FAIL"
			failed++
		}
		fmt.Fprintf(stdout, "%s %s %d/%d\n", status, e.Feature, pass, len(e.Cases))
	}
	fmt.Fprintf(stdout, "ai-eval: %d features, %d cases, %d features failing (mode=%s)\n", len(evals), total, failed, ai.ModeMock)
	if failed > 0 {
		return 1
	}
	return 0
}

func loadFeatures(repo fs.FS, name string) (map[string]Feature, error) {
	raw, err := fs.ReadFile(repo, name)
	if err != nil {
		return nil, fmt.Errorf("read features: %w", err)
	}
	var doc struct {
		Features []Feature `json:"features"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse features: %w", err)
	}
	out := make(map[string]Feature, len(doc.Features))
	for _, f := range doc.Features {
		out[f.ID] = f
	}
	return out, nil
}
