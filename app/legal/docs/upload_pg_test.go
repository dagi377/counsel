package docs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/server"
)

// memBucket is the cell bucket in memory.
type memBucket struct{ objects map[string][]byte }

func (b *memBucket) PutObject(_ context.Context, key string, r io.Reader, _ int64) error {
	body, err := io.ReadAll(r)
	b.objects[key] = body
	return err
}

func (b *memBucket) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.objects[key])), nil
}

func (b *memBucket) SignGet(context.Context, string, time.Duration) (string, error) { return "", nil }

// memRows is DocumentRows in memory: the folder tree, documents, versions and their keys.
type memRows struct {
	folders  map[docs.FolderID]string
	docs     []docs.Document
	versions map[string]docs.NewVersion // version id -> row
	current  map[string]kdocs.VersionID // document id -> version
	keys     map[string]string          // by|key -> document id
}

func newMemRows() *memRows {
	return &memRows{folders: map[docs.FolderID]string{folderA: matterA, folderWall: matterWall},
		versions: map[string]docs.NewVersion{}, current: map[string]kdocs.VersionID{}, keys: map[string]string{}}
}

const folderWall = "0190c000-0000-7000-8000-0000000000ff"

func (m *memRows) ByKey(_ context.Context, _ pgx.Tx, by, key string) (docs.Document, kdocs.VersionID, bool, error) {
	id, ok := m.keys[by+"|"+key]
	if !ok {
		return docs.Document{}, "", false, nil
	}
	for _, d := range m.docs {
		if d.ID == id {
			return d, m.current[id], true, nil
		}
	}
	return docs.Document{}, "", false, errors.New("dangling key")
}

func (m *memRows) FolderMatter(_ context.Context, _ pgx.Tx, f docs.FolderID) (string, error) {
	mt, ok := m.folders[f]
	if !ok {
		return "", docs.ErrNotFound
	}
	return mt, nil
}

func (m *memRows) Insert(_ context.Context, _ pgx.Tx, nv docs.NewVersion) (docs.Document, kdocs.VersionID, error) {
	n := len(m.docs) + 1
	d := docs.Document{ID: fmt.Sprintf("0190d000-0000-7000-8000-%012d", 500+n), MatterID: nv.Upload.MatterID,
		Kind: nv.Upload.Kind, FolderID: nv.Upload.FolderID}
	v := kdocs.VersionID(fmt.Sprintf("0190e000-0000-7000-8000-%012d", 500+n))
	m.docs = append(m.docs, d)
	m.versions[string(v)] = nv
	m.current[d.ID] = v
	if nv.Upload.IdempotencyKey != "" {
		m.keys[nv.Upload.By+"|"+nv.Upload.IdempotencyKey] = d.ID
	}
	return d, v, nil
}

func (m *memRows) List(context.Context, pgx.Tx, docs.ListFilter) ([]docs.Document, error) {
	return m.docs, nil
}

func (m *memRows) Get(_ context.Context, _ pgx.Tx, id string) (docs.Document, error) {
	for _, d := range m.docs {
		if d.ID == id {
			return d, nil
		}
	}
	return docs.Document{}, docs.ErrNotFound
}

func (m *memRows) HasVersion(_ context.Context, _ pgx.Tx, id string, v kdocs.VersionID) (bool, error) {
	return m.current[id] == v, nil
}

// memText records the text step's outcome per version.
type memText struct {
	text        map[kdocs.VersionID]string
	unavailable map[kdocs.VersionID]string
}

func (m *memText) Put(_ context.Context, _ pgx.Tx, v kdocs.VersionID, body []byte) error {
	m.text[v] = string(body)
	return nil
}

func (m *memText) MarkUnavailable(_ context.Context, _ pgx.Tx, v kdocs.VersionID, reason string) error {
	m.unavailable[v] = reason
	return nil
}

// extractModel is the mock model: every extraction answers out.
type extractModel struct {
	out   string
	calls int
}

func (m *extractModel) Complete(context.Context, ai.Request) (ai.Response, error) {
	m.calls++
	return ai.Response{JSON: json.RawMessage(m.out)}, nil
}

func (m *extractModel) Stream(context.Context, ai.Request) (<-chan ai.StreamEvent, error) {
	return nil, errors.New("not streamed")
}

func (m *extractModel) Mode() ai.Mode { return ai.ModeMock }

type uploadRig struct {
	store  *docs.PGDocuments
	rows   *memRows
	text   *memText
	bucket *memBucket
	events *outboxfake.Fake
	h      http.Handler
}

func newUploadRig(t *testing.T, model ai.ModelClient) uploadRig {
	t.Helper()
	r := uploadRig{rows: newMemRows(), bucket: &memBucket{objects: map[string][]byte{}}, events: outboxfake.New(),
		text: &memText{text: map[kdocs.VersionID]string{}, unavailable: map[kdocs.VersionID]string{}}}
	r.store = &docs.PGDocuments{DB: dbfake.New(), Blobs: kdocs.NewBlobs(r.bucket, nil), Text: r.text,
		Events: r.events, Rows: r.rows}
	if model != nil {
		r.store.Extractor = kdocs.ModelExtractor{Model: model, FeatureID: docs.FeatureIngestDocument}
	}
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	h := &docs.HTTP{Documents: r.store, Pages: kdocs.PGText{}, Access: testAccess{}, Cursors: codec,
		Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	r.h = m.Handler()
	return r
}

// TestPGDocumentsUpload: an upload stores its bytes in the bucket, one version, the version's
// text (or marks it text-unavailable) and exactly one document.uploaded with ids only.
//
// rule: ARC-U-012
// rule: ARC-I-030
func TestPGDocumentsUpload(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("%PDF-1.7 \x00binary")
	tests := []struct {
		name        string
		file        string
		content     []byte
		model       *extractModel
		text        string
		unavailable string
		mime        string
	}{
		{"text stored as is", "note.txt", []byte("Line one\nLine two"), nil, "Line one\nLine two", "", "text/plain; charset=utf-8"},
		{"pdf through the model", "scan.pdf", pdf, &extractModel{out: `{"available":true,"pages":["P1","P2"]}`}, "P1\fP2", "", "application/pdf"},
		{"pdf with no extractor", "scan.pdf", pdf, nil, "", kdocs.UnavailableNoExtractor, "application/pdf"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var model ai.ModelClient
			if tc.model != nil {
				model = tc.model
			}
			r := newUploadRig(t, model)
			d, v, err := r.store.Upload(ctx, docs.Upload{MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: folderA,
				Filename: tc.file, Content: tc.content, By: "0190b000-0000-7000-8000-000000000001", IdempotencyKey: key1})
			if err != nil {
				t.Fatal(err)
			}
			nv, ok := r.rows.versions[string(v)]
			if !ok || len(r.rows.versions) != 1 || nv.Mime != tc.mime || nv.Blob.Size != int64(len(tc.content)) {
				t.Fatalf("versions = %+v", r.rows.versions)
			}
			if got := r.bucket.objects[nv.Blob.Key]; !bytes.Equal(got, tc.content) {
				t.Fatalf("bucket object = %q", got)
			}
			if r.text.text[v] != tc.text || r.text.unavailable[v] != tc.unavailable {
				t.Fatalf("text %q unavailable %q", r.text.text[v], r.text.unavailable[v])
			}
			msgs := r.events.Messages()
			if len(msgs) != 1 || msgs[0].Type != docs.EventUploaded || msgs[0].Subject.ID != d.ID {
				t.Fatalf("events = %+v", msgs)
			}
			var body docs.Uploaded
			if err := json.Unmarshal(msgs[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			want := docs.Uploaded{DocumentID: d.ID, VersionID: string(v), MatterID: matterA, SHA256: nv.Blob.SHA256,
				Bytes: int64(len(tc.content)), Mime: tc.mime}
			if body != want {
				t.Fatalf("event body = %+v, want %+v", body, want)
			}
			// The event carries ids only, never the file's text or name.
			if strings.Contains(string(msgs[0].Payload), tc.file) || strings.Contains(string(msgs[0].Payload), "Line one") {
				t.Fatalf("event leaks content: %s", msgs[0].Payload)
			}
		})
	}
}

// TestPGDocumentsUploadRefusals: a bad upload stores nothing and emits nothing.
func TestPGDocumentsUploadRefusals(t *testing.T) {
	by := "0190b000-0000-7000-8000-000000000001"
	tests := []struct {
		name string
		u    docs.Upload
		want error
	}{
		{"too large", docs.Upload{MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: folderA, By: by,
			Content: make([]byte, docs.MaxDocumentBytes+1)}, docs.ErrInvalid},
		{"empty", docs.Upload{MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: folderA, By: by}, docs.ErrInvalid},
		{"unknown kind", docs.Upload{MatterID: matterA, Kind: "nope", FolderID: folderA, By: by, Content: []byte("x")},
			docs.ErrUnknownKind},
		{"folder of another matter", docs.Upload{MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: folderWall,
			By: by, Content: []byte("x")}, docs.ErrWrongMatter},
		{"unknown folder", docs.Upload{MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: "0190c000-0000-7000-8000-000000000999",
			By: by, Content: []byte("x")}, docs.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newUploadRig(t, nil)
			if _, _, err := r.store.Upload(context.Background(), tc.u); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(r.rows.docs) != 0 || len(r.events.Messages()) != 0 {
				t.Fatalf("stored %d documents, %d events", len(r.rows.docs), len(r.events.Messages()))
			}
		})
	}
	if _, _, err := (&docs.PGDocuments{}).Upload(context.Background(), docs.Upload{}); !errors.Is(err, docs.ErrNoWritePoint) {
		t.Fatalf("unwired err = %v", err)
	}
}

// TestPGDocumentsUploadHTTP: through POST /documents a walled matter answers 404 and a member
// without documents.edit 403, neither storing anything; the same Idempotency-Key twice stores
// one document with one event, through the route and through the store.
//
// rule: ARC-I-042
// rule: ARC-I-051
// rule: ARC-I-007
// rule: ARC-I-008
func TestPGDocumentsUploadHTTP(t *testing.T) {
	walled := `{"matter_id":"` + matterWall + `","folder_id":"` + folderWall + `","kind":"research_memo","content":"` + sampleBytes + `"}`
	tests := []struct {
		name   string
		user   string
		bodies []string
		status int
		docs   int
	}{
		{"walled matter is 404", "editor", []string{walled}, http.StatusNotFound, 0},
		{"missing capability is 403", "viewer", []string{uploadBody(docs.KindResearchMemo, "")}, http.StatusForbidden, 0},
		{"same key twice is one document", "editor", []string{uploadBody(docs.KindResearchMemo, ""),
			uploadBody(docs.KindResearchMemo, "")}, http.StatusCreated, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newUploadRig(t, nil)
			var ids []string
			for _, b := range tc.bodies {
				rec := do(r.h, http.MethodPost, "/v1/documents", tc.user, b, map[string]string{idem.HeaderName: key1})
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
				}
				var out struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &out)
				ids = append(ids, out.ID)
			}
			if len(r.rows.docs) != tc.docs || len(r.events.Messages()) != tc.docs || len(r.bucket.objects) != tc.docs {
				t.Fatalf("documents %d events %d objects %d, want %d", len(r.rows.docs), len(r.events.Messages()),
					len(r.bucket.objects), tc.docs)
			}
			if tc.docs == 1 && ids[0] != ids[1] {
				t.Fatalf("replay ids = %v", ids)
			}
		})
	}
	// The store dedups by creator and key too, without the route's replay.
	r := newUploadRig(t, nil)
	u := docs.Upload{MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: folderA, Filename: "a.txt",
		Content: []byte("x"), By: "0190b000-0000-7000-8000-000000000001", IdempotencyKey: key1}
	d1, v1, err1 := r.store.Upload(context.Background(), u)
	d2, v2, err2 := r.store.Upload(context.Background(), u)
	if err1 != nil || err2 != nil || d1.ID != d2.ID || v1 != v2 || len(r.rows.docs) != 1 || len(r.events.Messages()) != 1 {
		t.Fatalf("store replay: %v %v %v %v docs %d events %d", err1, err2, d1, d2, len(r.rows.docs), len(r.events.Messages()))
	}
}
