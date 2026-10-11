package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"strings"

	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// Embeddings protocol (spec/ai/mocks.md sec. 6): POST /v1/embeddings, OpenAI-compatible.
// Vectors are aifake.Vector (sha256 of the normalised text), shared with the in-process fake.
// A fixture of the request's feature can pin vectors by text, or group texts into clusters
// whose members sit close together.

// embedDim is the configured dimension; a request's "dimensions" overrides it.
var embedDim = flag.Int("embed-dim", 1024, "embedding dimension when a request names none")

// maxEmbedDim bounds a requested dimension.
const maxEmbedDim = 8192

// clusterSpread is how far a cluster member strays from its cluster centre. Members are
// normalise(centre + clusterSpread*own), so two members have cosine >= (1-s^2)/(1+s^2) ~ 0.956
// even in the worst case, and ~0.978 typically.
const clusterSpread = 0.15

type embeddingsRequest struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"` // a string or a list of strings
	Dimensions     int             `json:"dimensions"`
	EncodingFormat string          `json:"encoding_format"` // float (default) | base64
	User           string          `json:"user"`
}

type embeddingItem struct {
	Object    string `json:"object"`
	Index     int    `json:"index"`
	Embedding any    `json:"embedding"` // []float32 or a base64 string
}

// normText is the key texts are compared by, the same normalisation aifake.Vector hashes.
func normText(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func inputs(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("input: %w", err)
		}
		return []string{s}, nil
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err != nil {
		return nil, fmt.Errorf("input must be a string or a list of strings: %w", err)
	}
	if len(ss) == 0 {
		return nil, errors.New("input is empty")
	}
	return ss, nil
}

// pins holds a feature's pinned vectors and cluster membership by normalised text.
type pins struct {
	vectors map[string][]float32
	cluster map[string]string
}

func (s *server) pinsFor(feature string) (pins, error) {
	p := pins{vectors: map[string][]float32{}, cluster: map[string]string{}}
	if feature == "" {
		return p, nil
	}
	fxs, err := fixture.LoadFeature(s.fsys, feature)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return p, err
	}
	for _, fx := range fxs {
		for text, v := range fx.Vectors {
			p.vectors[normText(text)] = v
		}
		for name, members := range fx.Clusters {
			for _, m := range members {
				p.cluster[normText(m)] = feature + "/" + name
			}
		}
	}
	return p, nil
}

// vector is the embedding of text at dim: a pinned vector (zero-padded, normalised), else a
// cluster member near its cluster centre, else aifake.Vector.
func (p pins) vector(text string, dim int) ([]float32, error) {
	key := normText(text)
	if v, ok := p.vectors[key]; ok {
		if len(v) > dim {
			return nil, fmt.Errorf("pinned vector for %q has %d values, more than dimension %d", text, len(v), dim)
		}
		out := make([]float32, dim)
		copy(out, v)
		return normalise(out), nil
	}
	own := aifake.Vector(text, dim)
	name, ok := p.cluster[key]
	if !ok {
		return own, nil
	}
	centre := aifake.Vector("cluster:"+name, dim)
	out := make([]float32, dim)
	for i := range out {
		out[i] = centre[i] + clusterSpread*own[i]
	}
	return normalise(out), nil
}

func normalise(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

func encodeBase64(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func writeEmbeddingsError(w http.ResponseWriter, e *apiError) {
	body := map[string]any{"message": e.message, "type": e.typ, "code": e.typ, "param": nil}
	for k, v := range e.extra {
		body[k] = v
	}
	writeJSON(w, e.status, map[string]any{"error": body})
}

// featureFromUser reads a feature id prefix of the OpenAI "user" field ("F-ABC-01" or
// "F-ABC-01:anything").
func featureFromUser(user string) string {
	id, _, _ := strings.Cut(user, ":")
	if strings.HasPrefix(id, "F-") {
		return id
	}
	return ""
}

// embeddings answers POST /v1/embeddings with deterministic vectors; nothing here can reach a
// real model.
// rule: AI-R-001
func (s *server) embeddings(w http.ResponseWriter, r *http.Request) {
	bad := func(msg string) {
		writeEmbeddingsError(w, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: msg})
	}
	var q embeddingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&q); err != nil {
		bad("decode body: " + err.Error())
		return
	}
	texts, err := inputs(q.Input)
	if err != nil {
		bad(err.Error())
		return
	}
	dim := *embedDim
	if q.Dimensions != 0 {
		dim = q.Dimensions
	}
	if dim < 1 || dim > maxEmbedDim {
		bad(fmt.Sprintf("dimensions %d out of range 1..%d", dim, maxEmbedDim))
		return
	}
	if q.EncodingFormat != "" && q.EncodingFormat != "float" && q.EncodingFormat != "base64" {
		bad(fmt.Sprintf("encoding_format %q: want float or base64", q.EncodingFormat))
		return
	}
	feature := r.Header.Get("X-Counsel-Feature")
	if feature == "" {
		feature = featureFromUser(q.User)
	}
	p, err := s.pinsFor(feature)
	if err != nil {
		writeEmbeddingsError(w, &apiError{status: http.StatusInternalServerError, typ: "api_error", message: err.Error()})
		return
	}
	data := make([]embeddingItem, len(texts))
	tokens := 0
	for i, t := range texts {
		v, err := p.vector(t, dim)
		if err != nil {
			writeEmbeddingsError(w, &apiError{status: http.StatusInternalServerError, typ: "api_error", message: err.Error()})
			return
		}
		data[i] = embeddingItem{Object: "embedding", Index: i, Embedding: v}
		if q.EncodingFormat == "base64" {
			data[i].Embedding = encodeBase64(v)
		}
		tokens += estimateTokens(len(t))
	}
	slog.Info("llm fake: call answered", "protocol", "embeddings", "feature", feature, "inputs", len(texts), "dim", dim)
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data, "model": q.Model,
		"usage": map[string]int{"prompt_tokens": tokens, "total_tokens": tokens}})
}
