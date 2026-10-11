package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"

	"github.com/dagi377/counsel/kernel/ai"
)

// Candidate is one record to rank against a query. Text is client content: it goes to the
// embedder only and is never logged.
type Candidate struct {
	ID   string
	Text string
}

// Scored is a ranked candidate. Score is the cosine similarity to the query in ppm
// (-1_000_000..1_000_000), an integer like every other ratio in the system.
type Scored struct {
	Candidate
	Score int64
}

// ErrRank means the embedder answered with the wrong number of vectors.
var ErrRank = errors.New("run: rank: embedder returned a bad vector set")

// unit is the fixed-point scale vectors are quantised to before scoring.
const unit = 1_000_000

// Rank embeds query and every candidate in one call and returns the candidates by cosine
// score, highest first, ties by ID. No candidates means no model call. An embedder error
// returns no partial ranking.
func Rank(ctx context.Context, e ai.Embedder, featureID, query string, cands []Candidate) ([]Scored, error) {
	if len(cands) == 0 {
		return []Scored{}, nil
	}
	inputs := make([]string, 0, len(cands)+1)
	inputs = append(inputs, query)
	for _, c := range cands {
		inputs = append(inputs, c.Text)
	}
	resp, err := e.Embed(ctx, ai.EmbedRequest{FeatureID: featureID, Inputs: inputs})
	if err != nil {
		// rule: AI-R-005
		// Only the feature id and counts are logged; query and candidate text never are.
		slog.WarnContext(ctx, "rank embed failed", "feature", featureID, "candidates", len(cands))
		return nil, fmt.Errorf("rank %s: embed %d inputs: %w", featureID, len(inputs), err)
	}
	if len(resp.Vectors) != len(inputs) {
		return nil, fmt.Errorf("rank %s: %d vectors for %d inputs: %w", featureID, len(resp.Vectors), len(inputs), ErrRank)
	}
	// Vectors are quantised to fixed-point micro-units, so scoring is exact integer
	// arithmetic and the same vectors always rank the same way.
	vecs := make([][]int64, len(resp.Vectors))
	for i, v := range resp.Vectors {
		vecs[i] = make([]int64, len(v))
		for j, x := range v {
			vecs[i][j] = int64(x * unit)
		}
	}
	out := make([]Scored, len(cands))
	for i, c := range cands {
		out[i] = Scored{Candidate: c, Score: cosinePPM(vecs[0], vecs[i+1])}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	// rule: AI-R-005
	slog.InfoContext(ctx, "ranked", "feature", featureID, "candidates", len(cands))
	return out, nil
}

// cosinePPM is dot(a, b) / (|a| |b|) in ppm, rounded toward zero. A zero vector scores 0.
// Big integers keep the sums exact for any vector length.
func cosinePPM(a, b []int64) int64 {
	n := min(len(a), len(b))
	dot, na, nb := new(big.Int), new(big.Int), new(big.Int)
	t := new(big.Int)
	for i := range n {
		dot.Add(dot, t.Mul(big.NewInt(a[i]), big.NewInt(b[i])))
	}
	for _, x := range a {
		na.Add(na, t.Mul(big.NewInt(x), big.NewInt(x)))
	}
	for _, x := range b {
		nb.Add(nb, t.Mul(big.NewInt(x), big.NewInt(x)))
	}
	den := new(big.Int).Sqrt(new(big.Int).Mul(na, nb))
	if den.Sign() == 0 {
		return 0
	}
	num := new(big.Int).Mul(dot, big.NewInt(unit))
	return new(big.Int).Quo(num, den).Int64()
}
