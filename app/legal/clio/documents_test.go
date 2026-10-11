package clio

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"strconv"
	"testing"
	"time"
)

type zipEntry struct {
	name    string
	body    []byte
	symlink bool
}

func makeZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.symlink {
			h.SetMode(fs.ModeSymlink | 0o777)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// noise returns n bytes that do not compress, so the zip stays near its content size.
func noise(n int, seed byte) []byte {
	b := make([]byte, n)
	x := uint32(seed) + 1
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
	}
	return b
}

func hexSum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestPlanDocVersions(t *testing.T) {
	at := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	// rule: MIG-010
	src := []SourceDocVersion{
		{DocumentID: "d1", MatterID: "m1", Number: 3, Size: 15 << 10, UploadedAt: at.Add(2 * time.Hour)},
		{DocumentID: "d1", MatterID: "m1", Number: 1, Size: 10 << 10, UploadedAt: at},
		{DocumentID: "d1", MatterID: "m1", Number: 2, Size: 12 << 10, UploadedAt: at.Add(time.Hour)},
		{DocumentID: "d2", MatterID: "m1", Number: 1, Size: 600 << 20, UploadedAt: at},
	}
	got, err := PlanDocVersions(src, 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		doc     string
		n       int
		size    int64
		at      time.Time
		current bool
		state   DocVersionState
	}{
		{"d1", 1, 10 << 10, at, false, DocAvailable},
		{"d1", 2, 12 << 10, at.Add(time.Hour), false, DocAvailable},
		{"d1", 3, 15 << 10, at.Add(2 * time.Hour), true, DocAvailable},
		{"d2", 1, 600 << 20, at, true, DocPendingBytes}, // rule: MIG-011
	}
	if len(got) != len(want) {
		t.Fatalf("got %d versions, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.DocumentID != w.doc || g.Number != w.n || g.Size != w.size || !g.UploadedAt.Equal(w.at) || g.Current != w.current || g.State != w.state {
			t.Errorf("version %d = %+v, want %+v", i, g, w)
		}
	}

	bad := []struct {
		name string
		src  []SourceDocVersion
	}{
		{"zero version number", []SourceDocVersion{{DocumentID: "d", Number: 0}}},
		{"missing document", []SourceDocVersion{{Number: 1}}},
		{"duplicate number", []SourceDocVersion{{DocumentID: "d", Number: 1}, {DocumentID: "d", Number: 1}}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := PlanDocVersions(tc.src, 1); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestDocProgress(t *testing.T) {
	// rule: MIG-011
	tests := []struct {
		name     string
		versions []DocVersion
		want     MatterDocProgress
	}{
		{"two large videos arriving", []DocVersion{
			{DocumentID: "v1", MatterID: "m1", State: DocPendingBytes},
			{DocumentID: "v2", MatterID: "m1", State: DocPendingBytes},
			{DocumentID: "x", MatterID: "m2", State: DocPendingBytes},
		}, MatterDocProgress{Usable: true, Arriving: 2}},
		{"all available", []DocVersion{{DocumentID: "v1", MatterID: "m1", State: DocAvailable}}, MatterDocProgress{Usable: true}},
		{"two versions of one doc count once", []DocVersion{
			{DocumentID: "v1", MatterID: "m1", Number: 1, State: DocPendingBytes},
			{DocumentID: "v1", MatterID: "m1", Number: 2, State: DocPendingBytes},
		}, MatterDocProgress{Usable: true, Arriving: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DocProgress(tc.versions, "m1"); got != tc.want {
				t.Fatalf("DocProgress = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatchArchive120Docs(t *testing.T) {
	// rule: MIG-009
	var entries []zipEntry
	var versions []DocVersion
	for i := 0; i < 120; i++ {
		body := noise(64+i, byte(i))
		p := "matter/doc" + strconv.Itoa(i) + ".pdf"
		entries = append(entries, zipEntry{name: p, body: body})
		versions = append(versions, DocVersion{DocumentID: "d" + strconv.Itoa(i), MatterID: "m1", Path: p, Number: 1, Size: int64(len(body)), State: DocPendingBytes})
	}
	res, err := MatchArchive(makeZip(t, entries), versions, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 120 || len(res.Unmatched) != 0 || len(res.Issues) != 0 {
		t.Fatalf("matched %d, unmatched %d, issues %d; want 120, 0, 0", res.Matched, len(res.Unmatched), len(res.Issues))
	}
	for i, v := range res.Versions {
		if v.SHA256 != hexSum(entries[i].body) || v.State != DocAvailable {
			t.Fatalf("version %d = %+v", i, v)
		}
	}
	if versions[0].SHA256 != "" {
		t.Fatal("input versions were modified")
	}
}

func TestMatchArchiveEntries(t *testing.T) {
	// rule: MIG-009
	good := noise(200, 1)
	tests := []struct {
		name          string
		entries       []zipEntry
		versionSize   int64
		wantMatched   int
		wantIssues    []string
		wantUnmatched int
	}{
		{"parent path rejected, job continues",
			[]zipEntry{{name: "../etc/passwd", body: []byte("x")}, {name: "a.pdf", body: good}},
			int64(len(good)), 1, []string{"../etc/passwd"}, 0},
		{"absolute path rejected",
			[]zipEntry{{name: "/etc/passwd", body: []byte("x")}, {name: "a.pdf", body: good}},
			int64(len(good)), 1, []string{"/etc/passwd"}, 0},
		{"symlink rejected",
			[]zipEntry{{name: "link", body: []byte("/etc/passwd"), symlink: true}, {name: "a.pdf", body: good}},
			int64(len(good)), 1, []string{"link"}, 0},
		{"size mismatch is unmatched",
			[]zipEntry{{name: "a.pdf", body: good}},
			int64(len(good)) + 1, 0, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			versions := []DocVersion{{DocumentID: "d1", Path: "a.pdf", Number: 1, Size: tc.versionSize, State: DocPendingBytes}}
			res, err := MatchArchive(makeZip(t, tc.entries), versions, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			if res.Matched != tc.wantMatched || len(res.Unmatched) != tc.wantUnmatched || len(res.Issues) != len(tc.wantIssues) {
				t.Fatalf("result = %+v", res)
			}
			for i, e := range tc.wantIssues {
				if res.Issues[i].Entry != e {
					t.Fatalf("issue %d = %q, want %q", i, res.Issues[i].Entry, e)
				}
			}
		})
	}
}

func TestMatchArchiveBomb(t *testing.T) {
	// rule: MIG-009
	zeros := make([]byte, 900<<10) // compresses ~1000x: far past 4x the zip size
	bomb := makeZip(t, []zipEntry{{name: "big.bin", body: zeros}})
	small := makeZip(t, []zipEntry{{name: "a.bin", body: noise(1000, 2)}})
	tests := []struct {
		name     string
		zip      []byte
		maxTotal int64
		wantErr  error
	}{
		{"ratio above 4x rejected", bomb, 1 << 30, ErrArchiveBomb},
		{"above configured cap rejected", small, 500, ErrArchiveBomb},
		{"within limits accepted", small, 1 << 30, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MatchArchive(tc.zip, nil, tc.maxTotal)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
