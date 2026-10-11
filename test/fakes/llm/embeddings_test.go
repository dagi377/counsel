package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/kernel/ai/aifake"
)

func embedFS() fstest.MapFS {
	return fstest.MapFS{"f-emb-01/names/clusters.yaml": &fstest.MapFile{Data: []byte(`synthetic: true
feature: F-EMB-01
clusters:
  north: ["North Ltd", "NORTH Limited", "North Holdings"]
  south: ["South Inc", "South Incorporated"]
vectors:
  "pinned a": [1, 0, 0]
  "pinned b": [0.96, 0.28, 0]
`)}}
}

type embedResp struct {
	Data []struct {
		Index     int             `json:"index"`
		Embedding json.RawMessage `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

func embed(t *testing.T, hdr map[string]string, body map[string]any) [][]float32 {
	t.Helper()
	rec := post(t, newServer(embedFS(), nil).handler(), "/v1/embeddings", hdr, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var r embedResp
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	out := make([][]float32, len(r.Data))
	for i, d := range r.Data {
		if d.Index != i {
			t.Fatalf("data[%d].index = %d", i, d.Index)
		}
		if body["encoding_format"] == "base64" {
			var s string
			if err := json.Unmarshal(d.Embedding, &s); err != nil {
				t.Fatal(err)
			}
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				t.Fatal(err)
			}
			for j := 0; j < len(b); j += 4 {
				out[i] = append(out[i], math.Float32frombits(binary.LittleEndian.Uint32(b[j:])))
			}
			continue
		}
		if err := json.Unmarshal(d.Embedding, &out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return dot / math.Sqrt(na*nb)
}

func TestAI_R_001_EmbeddingSimilarity(t *testing.T) {
	// rule: AI-R-001
	feat := map[string]string{"X-Counsel-Feature": "F-EMB-01"}
	cases := []struct {
		name     string
		hdr      map[string]string
		a, b     string
		min, max float64
	}{
		{"identical texts", nil, "Acme Ltd", "Acme Ltd", 0.9999, 1.0001},
		{"identical after trim and case", nil, "  Acme Ltd ", "acme ltd", 0.9999, 1.0001},
		{"unrelated texts", nil, "Acme Ltd", "quarterly trust reconciliation", -0.2, 0.2},
		{"pinned near-duplicates", feat, "North Ltd", "NORTH Limited", 0.95, 1.0001},
		{"pinned near-duplicates 2", feat, "North Ltd", "north holdings", 0.95, 1.0001},
		{"different clusters", feat, "North Ltd", "South Inc", -0.2, 0.2},
		{"cluster member vs unrelated", feat, "South Incorporated", "Acme Ltd", -0.2, 0.2},
		{"pinned vectors", feat, "pinned a", "Pinned B", 0.95, 0.97},
		{"cluster ignored without feature", nil, "North Ltd", "NORTH Limited", -0.2, 0.2},
		{"feature from user prefix", nil, "North Ltd", "NORTH Limited", 0.95, 1.0001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"model": "embed", "input": []string{tc.a, tc.b}}
			if tc.name == "feature from user prefix" {
				body["user"] = "F-EMB-01:test"
			}
			v := embed(t, tc.hdr, body)
			if len(v) != 2 || len(v[0]) != 1024 {
				t.Fatalf("got %d vectors of dim %d, want 2 of 1024", len(v), len(v[0]))
			}
			if c := cosine(v[0], v[1]); c < tc.min || c > tc.max {
				t.Fatalf("cosine(%q, %q) = %.4f, want [%.2f, %.2f]", tc.a, tc.b, c, tc.min, tc.max)
			}
		})
	}
}

func TestEmbeddingsShape(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		wantDim int
	}{
		{"string input default dim", map[string]any{"input": "hello"}, 1024},
		{"dimensions honoured", map[string]any{"input": []string{"hello"}, "dimensions": 256}, 256},
		{"base64 encoding", map[string]any{"input": "hello", "encoding_format": "base64"}, 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := embed(t, nil, tc.body)
			if len(v) != 1 || len(v[0]) != tc.wantDim {
				t.Fatalf("dim %d, want %d", len(v[0]), tc.wantDim)
			}
			// Same function as the in-process fake (mocks.md sec. 6).
			want := aifake.Vector("hello", tc.wantDim)
			for i := range want {
				if v[0][i] != want[i] {
					t.Fatalf("value %d = %v, want aifake.Vector's %v", i, v[0][i], want[i])
				}
			}
		})
	}
	bad := []map[string]any{
		{"input": []string{}},
		{"input": 7},
		{"input": "x", "dimensions": -1},
		{"input": "x", "encoding_format": "hex"},
	}
	for i, b := range bad {
		if rec := post(t, newServer(embedFS(), nil).handler(), "/v1/embeddings", nil, b); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad request %d: status %d", i, rec.Code)
		}
	}
}
