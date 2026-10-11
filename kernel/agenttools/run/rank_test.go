package run_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

const rankFeature = "F-RANK-01"

func rankFake(opts ...aifake.Option) *aifake.Fake {
	return aifake.New(fstest.MapFS{"f-rank-01/ranking/ok.yaml": {Data: []byte(`synthetic: true
feature: F-RANK-01
vectors:
  "query text": [1, 0, 0]
  "exact match": [1, 0, 0]
  "half match b": [1, 1, 0]
  "half match a": [1, 0, 1]
  "unrelated": [0, 1, 0]
turns:
  - reply: {text: "ok"}
`)}}, opts...)
}

func ids(s []run.Scored) []string {
	var out []string
	for _, c := range s {
		out = append(out, c.ID)
	}
	return out
}

func TestAI_R_005_Rank(t *testing.T) {
	tests := []struct {
		name      string
		cands     []run.Candidate
		want      []string
		wantCalls int
	}{
		{"cosine order, ties by id", []run.Candidate{
			{ID: "d", Text: "unrelated"}, {ID: "c", Text: "half match b"},
			{ID: "b", Text: "half match a"}, {ID: "a", Text: "exact match"},
		}, []string{"a", "b", "c", "d"}, 1},
		{"no candidates, no model call", nil, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := rankFake()
			for range 2 { // deterministic across calls
				got, err := run.Rank(context.Background(), f, rankFeature, "query text", tt.cands)
				if err != nil {
					t.Fatalf("rank: %v", err)
				}
				if !slices.Equal(ids(got), tt.want) {
					t.Fatalf("order = %v, want %v", ids(got), tt.want)
				}
				if len(got) > 0 && (got[0].Score != 1_000_000 || got[1].Score != got[2].Score) {
					t.Errorf("scores = %+v, want exact 1_000_000 and a tie", got)
				}
			}
			if n := len(f.EmbedJournal()); n != 2*tt.wantCalls {
				t.Errorf("embed calls = %d, want %d", n, 2*tt.wantCalls)
			}
		})
	}
}

func TestAI_R_005_RankEmbedError(t *testing.T) {
	const secret = "Confidential Party Name"
	tests := []struct {
		name  string
		fault aifake.Fault
		want  error
	}{
		{"rate limit", aifake.FaultRateLimit, ai.ErrRateLimited},
		{"timeout", aifake.FaultTimeout, ai.ErrTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			got, err := run.Rank(context.Background(), rankFake(aifake.WithFault(rankFeature, tt.fault)), rankFeature, "query text",
				[]run.Candidate{{ID: "a", Text: secret}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if got != nil {
				t.Errorf("partial ranking returned: %v", got)
			}
			// rule: AI-R-005
			for _, s := range []string{logs.String(), err.Error()} {
				if strings.Contains(s, secret) || strings.Contains(s, "query text") {
					t.Errorf("candidate or query text leaked: %q", s)
				}
			}
			if !strings.Contains(logs.String(), rankFeature) {
				t.Errorf("log lacks the feature id: %q", logs.String())
			}
		})
	}
}
