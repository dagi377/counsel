package feature

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// MaxPromptChars is the longest feature prompt file, in characters.
const MaxPromptChars = 4000

// ErrPromptLint: a feature prompt or its fixture breaks the prompt rules.
var ErrPromptLint = errors.New("feature: prompt lint")

//go:embed prompts/rules.md
var rules string

// Rules returns the shared rules every feature prompt sits under (prompts/rules.md).
func Rules() string {
	return rules
}

var (
	promptHeading = regexp.MustCompile(`^# (F-[A-Z]+-\d{2})\b`)
	placeholder   = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	// declinePrompt is the heading of a decline or non-engagement letter prompt.
	declinePrompt = regexp.MustCompile(`(?i)\bdeclin|\bnon-engagement\b`)
	// limitationFigure is a limitation period stated or asked for: a duration ("2 years",
	// "two-year", "15 years") or an instruction to state the period or its date.
	limitationFigure = regexp.MustCompile(`(?i)\b(?:\d+|one|two|three|four|five|six|seven|ten|fifteen)[\s-]+(?:years?|months?|weeks?|days?)\b` +
		`|\blimitation (?:period|date|deadline)s? (?:of|is|ends|expires|runs out|on|the \w+ should watch)\b` +
		`|\b(?:state|give|include|compute|calculate)s? (?:the |any |a )?limitation (?:period|date|deadline)`)
	// nameValue is a person or organisation name as fixtures write them: two or more
	// capitalised words ("Avery Synth", "Synth Holdings Ltd").
	nameValue = regexp.MustCompile(`^[A-Z][\w'&.-]*( [A-Z&][\w'&.,-]*)+$`)
)

// nameKeys are the fixture keys whose values are client or party names.
var nameKeys = map[string]bool{
	"name": true, "party": true, "client": true, "client_name": true,
	"display_name": true, "full_name": true, "legal_name": true,
}

// LintPrompt checks d's prompt file: it starts with "# <feature id>", is at most
// MaxPromptChars characters, names every gate kind d may open, and mentions none of names
// (the synthetic client names its fixtures use) outside {{placeholders}}.
func LintPrompt(d Def, names []string) error {
	if d.Prompt == nil || d.PromptPath == "" {
		return fmt.Errorf("feature %s: prompt required: %w", d.ID, ErrPromptLint)
	}
	b, err := fs.ReadFile(d.Prompt, d.PromptPath)
	if err != nil {
		return fmt.Errorf("feature %s: prompt: %w", d.ID, err)
	}
	text := string(b)
	m := promptHeading.FindStringSubmatch(text)
	if m == nil || m[1] != d.ID {
		return fmt.Errorf("feature %s: prompt must start with %q: %w", d.ID, "# "+d.ID, ErrPromptLint)
	}
	if n := utf8.RuneCountInString(text); n > MaxPromptChars {
		return fmt.Errorf("feature %s: prompt is %d chars, max %d: %w", d.ID, n, MaxPromptChars, ErrPromptLint)
	}
	for _, k := range d.GateKinds {
		if !strings.Contains(text, k) {
			return fmt.Errorf("feature %s: prompt does not name gate kind %q: %w", d.ID, k, ErrPromptLint)
		}
	}
	// rule: AI-R-004
	// A decline or non-engagement letter goes to a person the firm does not act for: it may
	// warn that time limits may apply, but it never states or estimates a limitation period,
	// which would be advice they could rely on.
	if title, _, _ := strings.Cut(text, "\n"); declinePrompt.MatchString(title) && limitationFigure.MatchString(text) {
		return fmt.Errorf("feature %s: decline or non-engagement prompt states a limitation period: %w", d.ID, ErrPromptLint)
	}
	// rule: AI-R-004
	// A client name belongs in a {{placeholder}} the run fills, never in the prompt text, so a
	// committed prompt holds no client data and its hash does not change per client.
	bare := strings.ToLower(placeholder.ReplaceAllString(text, " "))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" && strings.Contains(bare, strings.ToLower(n)) {
			// The name itself stays out of the error: it is client-shaped data.
			return fmt.Errorf("feature %s: prompt holds a fixture client name outside {{placeholders}}: %w", d.ID, ErrPromptLint)
		}
	}
	return nil
}

// PromptHash is the fixture prompt hash of d's run spec with no messages: the feature id,
// the system prompt and the tools it offers. A fixture's ok variant may pin it in
// match.prompt_hash so a prompt change is noticed.
func PromptHash(d Def) (string, error) {
	spec, err := d.Spec()
	if err != nil {
		return "", err
	}
	h, err := fixture.PromptHash(fixture.Prompt{Feature: spec.FeatureID, System: spec.System, Tools: spec.Tools})
	if err != nil {
		return "", fmt.Errorf("feature %s: %w", d.ID, err)
	}
	return h, nil
}

// LintFixtureHash fails when the ok variant in d's fixture dir (fixtures is rooted where
// d.Fixture is relative to) pins a match.prompt_hash that differs from PromptHash(d). An
// empty prompt_hash (match by regex) passes.
func LintFixtureHash(d Def, fixtures fs.FS) error {
	ok, err := fixture.Load(fixtures, path.Join(d.Fixture, fixture.VariantOK+".yaml"))
	if err != nil {
		return fmt.Errorf("feature %s: %w", d.ID, err)
	}
	if ok.Match.PromptHash == "" {
		return nil
	}
	h, err := PromptHash(d)
	if err != nil {
		return err
	}
	if ok.Match.PromptHash != h {
		return fmt.Errorf("feature %s: ok fixture prompt_hash is stale (rehash after review): %w", d.ID, ErrPromptLint)
	}
	return nil
}

// FixtureNames returns the client and party names the YAML files under dir use (inputs/
// included), for LintPrompt.
func FixtureNames(fixtures fs.FS, dir string) ([]string, error) {
	var names []string
	err := fs.WalkDir(fixtures, dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || (path.Ext(p) != ".yaml" && path.Ext(p) != ".yml") {
			return nil
		}
		b, err := fs.ReadFile(fixtures, p)
		if err != nil {
			return err
		}
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		collectNames(v, &names)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("fixture names %s: %w", dir, err)
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func collectNames(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			if s, ok := c.(string); ok && nameKeys[k] && nameValue.MatchString(s) {
				*out = append(*out, s)
				continue
			}
			collectNames(c, out)
		}
	case []any:
		for _, c := range x {
			collectNames(c, out)
		}
	}
}
