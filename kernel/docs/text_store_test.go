package docs_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/docs"
)

// memText is a TextWriter that keeps what the pipeline stored per version.
type memText struct {
	text        map[docs.VersionID]string
	unavailable map[docs.VersionID]string
	err         error
}

func newMemText() *memText {
	return &memText{text: map[docs.VersionID]string{}, unavailable: map[docs.VersionID]string{}}
}

func (m *memText) Put(_ context.Context, _ pgx.Tx, v docs.VersionID, body []byte) error {
	if m.err != nil {
		return m.err
	}
	m.text[v] = string(body)
	return nil
}

func (m *memText) MarkUnavailable(_ context.Context, _ pgx.Tx, v docs.VersionID, reason string) error {
	if m.err != nil {
		return m.err
	}
	m.unavailable[v] = reason
	return nil
}

// mockModel is the mock ModelClient: it answers every extraction with out, or err.
type mockModel struct {
	out  string
	err  error
	reqs []ai.Request
}

func (m *mockModel) Complete(_ context.Context, req ai.Request) (ai.Response, error) {
	m.reqs = append(m.reqs, req)
	if m.err != nil {
		return ai.Response{}, m.err
	}
	return ai.Response{JSON: json.RawMessage(m.out)}, nil
}

func (m *mockModel) Stream(context.Context, ai.Request) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not streamed")
}

func (m *mockModel) Mode() ai.Mode { return ai.ModeMock }

// TestStoreText: text and Markdown are stored as they are; other formats go through the
// model extraction; a file whose text cannot be had is marked text-unavailable, never stored
// empty.
//
// rule: ARC-I-037
func TestStoreText(t *testing.T) {
	ctx := context.Background()
	const v = docs.VersionID("v1")
	pdf := []byte("%PDF-1.7 \x00\x01binary")
	tests := []struct {
		name        string
		file        string
		content     []byte
		model       *mockModel // nil: no extractor
		stored      bool
		text        string
		unavailable string
		calls       int
	}{
		{"plain text stored directly", "note.txt", []byte("Line one\nLine two"), &mockModel{}, true, "Line one\nLine two", "", 0},
		{"markdown stored directly", "brief.MD", []byte("\xef\xbb\xbf# Facts\nSigned"), &mockModel{}, true, "# Facts\nSigned", "", 0},
		{"pdf through model extraction", "scan.pdf", pdf, &mockModel{out: `{"available":true,"pages":["Page one","Page two"]}`}, true, "Page one\fPage two", "", 1},
		{"model finds no text", "photo.png", pdf, &mockModel{out: `{"available":false,"pages":[]}`}, false, "", docs.UnavailableNoText, 1},
		{"model fails", "scan.pdf", pdf, &mockModel{err: ai.ErrOverloaded}, false, "", docs.UnavailableExtraction, 1},
		{"no extractor", "scan.pdf", pdf, nil, false, "", docs.UnavailableNoExtractor, 0},
		{"empty text file", "empty.txt", []byte("  \n"), &mockModel{}, false, "", docs.UnavailableNoText, 0},
		{"binary named .txt goes to extraction", "odd.txt", pdf, &mockModel{out: `{"available":true,"pages":["Recovered"]}`}, true, "Recovered", "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newMemText()
			var ex docs.Extractor
			if tc.model != nil {
				ex = docs.ModelExtractor{Model: tc.model, FeatureID: "F-DOC-01"}
			}
			stored, err := docs.StoreText(ctx, nil, w, ex, v, tc.file, tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if stored != tc.stored || w.text[v] != tc.text || w.unavailable[v] != tc.unavailable {
				t.Fatalf("stored %v text %q unavailable %q", stored, w.text[v], w.unavailable[v])
			}
			if tc.model != nil && len(tc.model.reqs) != tc.calls {
				t.Fatalf("model calls = %d, want %d", len(tc.model.reqs), tc.calls)
			}
			if tc.calls > 0 {
				req := tc.model.reqs[0]
				// rule: ARC-I-038
				// The file reaches the model only as quoted tool_result data.
				if req.Kind != ai.KindExtraction || len(req.Schema) == 0 || strings.Contains(req.Messages[0].Content[0].Text, "PDF") {
					t.Fatalf("request = %+v", req)
				}
			}
		})
	}
}

// A failing text store fails the step, so the upload does not commit a version without text.
func TestStoreTextStoreFails(t *testing.T) {
	w := newMemText()
	w.err = errors.New("db down")
	if _, err := docs.StoreText(context.Background(), nil, w, nil, "v1", "a.txt", []byte("x")); !errors.Is(err, w.err) {
		t.Fatalf("err = %v", err)
	}
	if _, err := docs.StoreText(context.Background(), nil, nil, nil, "v1", "a.txt", []byte("x")); !errors.Is(err, docs.ErrNotFound) {
		t.Fatalf("no store err = %v", err)
	}
}

// ErrTextUnavailable fails closed for readers that treat a missing version as not found.
func TestErrTextUnavailableIsNotFound(t *testing.T) {
	if !errors.Is(docs.ErrTextUnavailable, docs.ErrNotFound) {
		t.Fatal("ErrTextUnavailable must wrap ErrNotFound")
	}
}
