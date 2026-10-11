package agenttools_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
)

func TestValidCitation(t *testing.T) {
	cases := []struct {
		name string
		c    agenttools.Citation
		ok   bool
	}{
		{"one line", agenttools.Citation{VersionID: "v1", Page: 1, FromLine: 3, ToLine: 3}, true},
		{"range", agenttools.Citation{VersionID: "v1", Page: 4, FromLine: 3, ToLine: 9}, true},
		{"no version", agenttools.Citation{Page: 1, FromLine: 1, ToLine: 1}, false},
		{"page zero", agenttools.Citation{VersionID: "v1", Page: 0, FromLine: 1, ToLine: 1}, false},
		{"line zero", agenttools.Citation{VersionID: "v1", Page: 1, FromLine: 0, ToLine: 1}, false},
		{"reversed range", agenttools.Citation{VersionID: "v1", Page: 1, FromLine: 5, ToLine: 4}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := agenttools.ValidCitation(tc.c)
			if tc.ok && err != nil {
				t.Fatalf("ValidCitation: %v", err)
			}
			if !tc.ok && !errors.Is(err, agenttools.ErrCitation) {
				t.Fatalf("err = %v, want ErrCitation", err)
			}
		})
	}
}

func TestCheckCitationsQuotedResults(t *testing.T) {
	good := agenttools.Citation{VersionID: "v1", Page: 2, FromLine: 4, ToLine: 6}
	other := agenttools.Citation{VersionID: "v2", Page: 1, FromLine: 1, ToLine: 2}
	quotes := func(qs ...agenttools.Quoted) json.RawMessage {
		b, err := json.Marshal(map[string]any{"quotes": qs})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	q := func(c agenttools.Citation) agenttools.Quoted {
		return agenttools.Quoted{Untrusted: true, Source: c, Text: "passage"}
	}
	cases := []struct {
		name string
		res  agenttools.ToolResult
		ok   bool
	}{
		{"no quotes no citations", agenttools.ToolResult{Output: json.RawMessage(`{"count":2}`)}, true},
		{"empty output", agenttools.ToolResult{}, true},
		{"non-object output", agenttools.ToolResult{Output: json.RawMessage(`[1,2]`)}, true},
		{"quote cited", agenttools.ToolResult{Output: quotes(q(good)), Citations: []agenttools.Citation{good}}, true},
		{"two quotes cited", agenttools.ToolResult{Output: quotes(q(good), q(other)), Citations: []agenttools.Citation{other, good}}, true},
		{"quote without citation", agenttools.ToolResult{Output: quotes(q(good))}, false},
		{"quote cites other passage", agenttools.ToolResult{Output: quotes(q(good)), Citations: []agenttools.Citation{other}}, false},
		{"second quote uncited", agenttools.ToolResult{Output: quotes(q(good), q(other)), Citations: []agenttools.Citation{good}}, false},
		{"quote with bad shape", agenttools.ToolResult{Output: quotes(q(agenttools.Citation{VersionID: "v1"})), Citations: []agenttools.Citation{{VersionID: "v1"}}}, false},
		{"bad citation listed", agenttools.ToolResult{Output: json.RawMessage(`{}`), Citations: []agenttools.Citation{{Page: 1, FromLine: 1, ToLine: 1}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := agenttools.CheckCitations(tc.res)
			if tc.ok && err != nil {
				t.Fatalf("CheckCitations: %v", err)
			}
			if !tc.ok && !errors.Is(err, agenttools.ErrCitation) {
				t.Fatalf("err = %v, want ErrCitation", err)
			}
		})
	}
}

func TestCheckCitationsRejectsMalformedOutput(t *testing.T) {
	for _, out := range []string{`{"quotes":[`, `{"quotes":"v1 p2"}`, `{"quotes":[{"source":"v1"}]}`} {
		err := agenttools.CheckCitations(agenttools.ToolResult{Output: json.RawMessage(out)})
		if !errors.Is(err, agenttools.ErrCitation) {
			t.Fatalf("%s: err = %v, want ErrCitation", out, err)
		}
	}
}
