package aifake

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"

	"github.com/dagi377/counsel/kernel/ai"
	aifixture "github.com/dagi377/counsel/kernel/ai/fixture"
)

// clusterSpread is the share of a cluster member's vector that is its own; the rest is the
// cluster centre. Two members then have cos >= 1 - 2*clusterSpread^2 (0.96 here), and the
// centres of different clusters, like unrelated texts, are independent hash vectors.
const clusterSpread = 0.14

// embedKey is the text normalisation shared by Vector and fixture pins.
func embedKey(text string) string { return strings.ToLower(strings.TrimSpace(text)) }

// pins are a feature's embedding pins: vectors by text, and the cluster each text sits in.
type pins struct {
	vectors  map[string][]float32
	clusters map[string]string
}

// embedPins loads the pinned vectors and clusters from featureID's fixtures. A feature with
// no fixture directory has no pins. A cluster name is one cluster across the feature's
// variants. When two fixtures pin one text, the first by path wins.
func (f *Fake) embedPins(featureID string) (pins, error) {
	p := pins{vectors: map[string][]float32{}, clusters: map[string]string{}}
	fxs, err := aifixture.LoadFeature(f.fsys, featureID)
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("aifake: embed fixtures %s: %w", featureID, err)
	}
	for _, fx := range fxs {
		for text, v := range fx.Vectors {
			if _, ok := p.vectors[embedKey(text)]; !ok {
				p.vectors[embedKey(text)] = v
			}
		}
		for name, texts := range fx.Clusters {
			for _, text := range texts {
				if _, ok := p.clusters[embedKey(text)]; !ok {
					p.clusters[embedKey(text)] = name
				}
			}
		}
	}
	return p, nil
}

// pinned returns v zero-padded to dim and normalised.
func pinned(v []float32, dim int) ([]float32, error) {
	if len(v) > dim {
		return nil, fmt.Errorf("aifake: pinned vector has %d values, dim is %d", len(v), dim)
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil, fmt.Errorf("aifake: pinned vector is zero")
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dim)
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out, nil
}

// clustered places text near its cluster centre: sqrt(1-s^2)*centre + s*u, where u is the
// text's own hash vector made orthogonal to the centre.
func clustered(text, cluster string, dim int) []float32 {
	c := Vector("\x00cluster\x00"+cluster, dim)
	t := Vector(text, dim)
	var dot float64
	for i := range c {
		dot += float64(c[i]) * float64(t[i])
	}
	u := make([]float64, dim)
	var un float64
	for i := range u {
		u[i] = float64(t[i]) - dot*float64(c[i])
		un += u[i] * u[i]
	}
	un = math.Sqrt(un)
	a := math.Sqrt(1 - clusterSpread*clusterSpread)
	v := make([]float64, dim)
	var norm float64
	for i := range v {
		v[i] = a * float64(c[i])
		if un > 0 {
			v[i] += clusterSpread * u[i] / un
		}
		norm += v[i] * v[i]
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dim)
	for i, x := range v {
		out[i] = float32(x / norm)
	}
	return out
}

// Embed returns one unit vector per input (spec/ai/mocks.md sec. 6). A text pinned in the
// feature's fixtures gets its pinned vector, a text in a fixture cluster sits near the other
// members, and any other text gets the deterministic hash Vector. Texts match after
// lowercase(trim(text)).
func (f *Fake) Embed(ctx context.Context, req ai.EmbedRequest) (ai.EmbedResponse, error) {
	if err := ctx.Err(); err != nil {
		return ai.EmbedResponse{}, err
	}
	if err := req.Validate(); err != nil {
		return ai.EmbedResponse{}, err
	}
	if fault, ok := f.faults[req.FeatureID]; ok {
		return ai.EmbedResponse{}, faultError(fault)
	}
	// rule: AI-R-002
	p, err := f.embedPins(req.FeatureID)
	if err != nil {
		return ai.EmbedResponse{}, err
	}
	resp := ai.EmbedResponse{Vectors: make([][]float32, len(req.Inputs))}
	for i, in := range req.Inputs {
		key := embedKey(in)
		switch v, ok := p.vectors[key]; {
		case ok:
			if resp.Vectors[i], err = pinned(v, f.dim); err != nil {
				return ai.EmbedResponse{}, err
			}
		case p.clusters[key] != "":
			resp.Vectors[i] = clustered(key, p.clusters[key], f.dim)
		default:
			resp.Vectors[i] = Vector(in, f.dim)
		}
		resp.Usage.InputTokens += (len(in) + 3) / 4
	}
	return resp, nil
}
