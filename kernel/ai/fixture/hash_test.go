package fixture

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/canon"
)

type block struct {
	Type      string
	Text      string
	ToolUseID string
	Input     json.RawMessage
	IsError   bool
}

type message struct {
	Role    string
	Content []block
}

func userText(s string) []message {
	return []message{{Role: "user", Content: []block{{Type: "text", Text: s}}}}
}

func mustHash(t *testing.T, p Prompt) string {
	t.Helper()
	h, err := PromptHash(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestPromptHash(t *testing.T) {
	base := Prompt{Feature: "F-TST-01", System: "sys", Messages: userText("rebuild day"), Tools: []map[string]string{{"name": "report_plan"}}}
	h := mustHash(t, base)
	if len(h) != 64 {
		t.Fatalf("hash %q is not hex sha256", h)
	}
	toolResult := func(id string) []message {
		return []message{{Role: "tool", Content: []block{{Type: "tool_result", ToolUseID: id, Text: "ok"}}}}
	}
	tests := []struct {
		name string
		a, b Prompt
		same bool
	}{
		{name: "deterministic", a: base, b: base, same: true},
		{name: "ulid masked",
			a: Prompt{Feature: "F-TST-01", Messages: userText("item 01JA2B3C4D5E6F7G8H9JKMNPQR")},
			b: Prompt{Feature: "F-TST-01", Messages: userText("item 01HZZZZZZZZZZZZZZZZZZZZZZZ")}, same: true},
		{name: "timestamp masked",
			a: Prompt{Feature: "F-TST-01", Messages: userText("at 2026-10-09T09:00:00-04:00")},
			b: Prompt{Feature: "F-TST-01", Messages: userText("at 2026-10-11T17:30:12.5Z")}, same: true},
		{name: "seed masked like the seeded id",
			a: Prompt{Feature: "F-TST-01", Messages: userText("for {{seed.item}}")},
			b: Prompt{Feature: "F-TST-01", Messages: userText("for <seed>")}, same: true},
		{name: "tool use id masked",
			a: Prompt{Feature: "F-TST-01", Messages: toolResult("toolu_1")},
			b: Prompt{Feature: "F-TST-01", Messages: toolResult("call_9")}, same: true},
		{name: "go struct and snake case omitempty encoding agree",
			a: Prompt{Feature: "F-TST-01", Messages: toolResult("x")},
			b: Prompt{Feature: "F-TST-01", Messages: json.RawMessage(`[{"role":"tool","content":[{"type":"tool_result","tool_use_id":"y","text":"ok"}]}]`)}, same: true},
		{name: "key order irrelevant",
			a: Prompt{Feature: "F-TST-01", Tools: json.RawMessage(`[{"name":"a","description":"d"}]`)},
			b: Prompt{Feature: "F-TST-01", Tools: json.RawMessage(`[{"description":"d","name":"a"}]`)}, same: true},
		{name: "bare date is content", a: Prompt{Feature: "F-TST-01", Messages: userText("2026-10-09")},
			b: Prompt{Feature: "F-TST-01", Messages: userText("2026-10-10")}, same: false},
		{name: "text change", a: base, b: Prompt{Feature: "F-TST-01", System: "sys", Messages: userText("rebuild week"), Tools: base.Tools}, same: false},
		{name: "system change", a: base, b: Prompt{Feature: "F-TST-01", System: "sys2", Messages: base.Messages, Tools: base.Tools}, same: false},
		{name: "feature change", a: base, b: Prompt{Feature: "F-TST-02", System: "sys", Messages: base.Messages, Tools: base.Tools}, same: false},
		{name: "tools change", a: base, b: Prompt{Feature: "F-TST-01", System: "sys", Messages: base.Messages}, same: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mustHash(t, tt.a) == mustHash(t, tt.b); got != tt.same {
				t.Fatalf("same hash = %v, want %v", got, tt.same)
			}
		})
	}
}

func TestPromptHashPinned(t *testing.T) {
	// Pinned so a change to masking or canonical form is a deliberate, reviewed rehash.
	// sha256 of {"feature":"F-TST-01","messages":[{"content":[{"text":"hi","type":"text"}],"role":"user"}]}
	const want = "960ba11e7243a8ae99bab34f1aeb955056696fc1cbc3fbe14e4728f7a6eaf7b9"
	got := mustHash(t, Prompt{Feature: "F-TST-01", Messages: userText("hi")})
	if got != want {
		t.Fatalf("PromptHash = %s, want %s", got, want)
	}
}

// PromptHash uses kernel/canon's literal-number mode: a number hashes by its normalised
// literal, so decimals in prompts are accepted and equal values written differently match.
func TestPromptHashNumbers(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string // Tools JSON
		same    bool
		wantErr bool
	}{
		{name: "minus zero is zero", a: `[{"n":-0}]`, b: `[{"n":0}]`, same: true},
		{name: "integer spacing", a: `[{"n": 100}]`, b: `[{"n":100}]`, same: true},
		{name: "integer differs", a: `[{"n":100}]`, b: `[{"n":101}]`},
		{name: "decimal accepted", a: `[{"n":0.5}]`, b: `[{"n":0.50}]`, same: true},
		{name: "integral fraction is the integer", a: `[{"n":1.0}]`, b: `[{"n":1}]`, same: true},
		{name: "exponent normalised", a: `[{"n":1e21}]`, b: `[{"n":10E+20}]`, same: true},
		{name: "beyond 2^53-1 kept exactly", a: `[{"n":9007199254740993}]`, b: `[{"n":9007199254740992}]`},
		{name: "decimal differs", a: `[{"n":0.1}]`, b: `[{"n":0.10000000000000001}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha, err := PromptHash(Prompt{Feature: "F-TST-01", Tools: json.RawMessage(tt.a)})
			if tt.wantErr {
				if !errors.Is(err, canon.ErrNumber) {
					t.Fatalf("PromptHash err = %v, want canon.ErrNumber", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := ha == mustHash(t, Prompt{Feature: "F-TST-01", Tools: json.RawMessage(tt.b)}); got != tt.same {
				t.Fatalf("same hash = %v, want %v", got, tt.same)
			}
		})
	}
}
