package clio

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// ErrArchiveBomb is returned when an archive would expand past its limits (MIG-009).
var ErrArchiveBomb = errors.New("clio: document archive expands past its limits")

// archiveRatio is the largest allowed uncompressed / zip size ratio (MIG-009).
const archiveRatio = 4

// DocVersionState is whether a version's bytes are in Counsel yet.
type DocVersionState string

// Version states (MIG-011).
const (
	DocAvailable    DocVersionState = "available"
	DocPendingBytes DocVersionState = "pending_bytes"
)

// SourceDocVersion is one Clio document version as listed by the source.
type SourceDocVersion struct {
	DocumentID string
	MatterID   string
	Path       string // path of the file inside the matter's document archive
	Number     int    // Clio version number
	Size       int64  // bytes
	UploadedAt time.Time
}

// DocVersion is one version as Counsel imports it.
type DocVersion struct {
	DocumentID string
	MatterID   string
	Path       string
	Number     int // the original Clio version number
	Size       int64
	UploadedAt time.Time
	Current    bool // the highest Clio version of the document
	State      DocVersionState
	SHA256     string // hex; set once the bytes are matched
}

// PlanDocVersions turns the source listing into the versions to import: every version keeps
// its original number and upload time, ordered by document then number, and the highest
// number is current. Versions above inlineBytes wait for their bytes in the background.
func PlanDocVersions(src []SourceDocVersion, inlineBytes int64) ([]DocVersion, error) {
	out := make([]DocVersion, 0, len(src))
	seen := map[string]bool{}
	for _, s := range src {
		if s.DocumentID == "" || s.Number < 1 || s.Size < 0 {
			return nil, fmt.Errorf("plan document versions: document %q version %d: invalid", s.DocumentID, s.Number)
		}
		key := fmt.Sprintf("%s\x00%d", s.DocumentID, s.Number)
		if seen[key] {
			return nil, fmt.Errorf("plan document versions: document %q version %d: duplicate", s.DocumentID, s.Number)
		}
		seen[key] = true
		// rule: MIG-010
		v := DocVersion{DocumentID: s.DocumentID, MatterID: s.MatterID, Path: s.Path, Number: s.Number, Size: s.Size, UploadedAt: s.UploadedAt}
		// rule: MIG-011
		if s.Size > inlineBytes {
			v.State = DocPendingBytes
		} else {
			v.State = DocAvailable
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DocumentID != out[j].DocumentID {
			return out[i].DocumentID < out[j].DocumentID
		}
		return out[i].Number < out[j].Number
	})
	// rule: MIG-010
	for i := range out {
		out[i].Current = i == len(out)-1 || out[i+1].DocumentID != out[i].DocumentID
	}
	return out, nil
}

// MatterDocProgress says whether a matter is usable and how many documents are still arriving.
type MatterDocProgress struct {
	Usable   bool // metadata imported; bytes may still be arriving
	Arriving int  // documents with at least one version in pending_bytes
}

// DocProgress reports a matter's progress once its version metadata is imported: the matter
// is usable at once, and shows how many documents are still arriving (MIG-011).
func DocProgress(versions []DocVersion, matterID string) MatterDocProgress {
	// rule: MIG-011
	pending := map[string]bool{}
	for _, v := range versions {
		if v.MatterID == matterID && v.State == DocPendingBytes {
			pending[v.DocumentID] = true
		}
	}
	return MatterDocProgress{Usable: true, Arriving: len(pending)}
}

// ArchiveIssue is one archive entry that was not imported. The job continues past it.
type ArchiveIssue struct {
	Entry  string
	Reason string
}

// ArchiveResult is what matching one matter archive produced. Versions is the input with
// SHA256 set and State flipped to available for every matched version.
type ArchiveResult struct {
	Versions  []DocVersion
	Matched   int
	Unmatched []string // archive entries with no version of the same path and size
	Issues    []ArchiveIssue
}

// MatchArchive reads one matter's Document Archives zip in a sandbox and matches each file
// to a document version by path and byte size, recording its sha256. Unsafe entries
// (symlinks, absolute or '..' paths) are skipped as issues. An archive whose declared or
// actual uncompressed total exceeds 4x the zip size or maxTotal is rejected whole. Nothing
// is written to disk.
func MatchArchive(zipBytes []byte, versions []DocVersion, maxTotal int64) (ArchiveResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return ArchiveResult{}, fmt.Errorf("match archive: open zip: %w", err)
	}
	limit := int64(len(zipBytes)) * archiveRatio
	if maxTotal < limit {
		limit = maxTotal
	}
	// rule: MIG-009
	var declared uint64
	for _, f := range zr.File {
		declared += f.UncompressedSize64
		if declared > uint64(limit) {
			return ArchiveResult{}, fmt.Errorf("match archive: declared size: %w", ErrArchiveBomb)
		}
	}
	res := ArchiveResult{Versions: append([]DocVersion(nil), versions...)}
	byKey := map[string]int{}
	for i, v := range res.Versions {
		byKey[fmt.Sprintf("%s\x00%d", v.Path, v.Size)] = i
	}
	var read int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, reason := safeEntry(f)
		if reason != "" {
			res.Issues = append(res.Issues, ArchiveIssue{Entry: f.Name, Reason: reason})
			continue
		}
		sum, n, err := hashEntry(f, limit-read)
		if err != nil {
			return ArchiveResult{}, fmt.Errorf("match archive: entry %q: %w", name, err)
		}
		read += n
		i, ok := byKey[fmt.Sprintf("%s\x00%d", name, n)]
		if !ok {
			res.Unmatched = append(res.Unmatched, name)
			continue
		}
		res.Versions[i].SHA256 = sum
		res.Versions[i].State = DocAvailable
		res.Matched++
	}
	return res, nil
}

// safeEntry returns the cleaned entry name, or a reason the entry is unsafe.
func safeEntry(f *zip.File) (string, string) {
	// rule: MIG-009
	if f.Mode()&fs.ModeSymlink != 0 {
		return "", "symlink"
	}
	n := strings.ReplaceAll(f.Name, "\\", "/")
	if strings.HasPrefix(n, "/") || (len(n) > 1 && n[1] == ':') {
		return "", "absolute path"
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", "parent path"
		}
	}
	return path.Clean(n), ""
}

// hashEntry reads at most budget bytes of the entry and returns its hex sha256 and length.
// Reading past budget means the entry lied about its size: the archive is a bomb.
func hashEntry(f *zip.File, budget int64) (string, int64, error) {
	rc, err := f.Open()
	if err != nil {
		return "", 0, fmt.Errorf("open: %w", err)
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(rc, budget+1))
	if err != nil {
		return "", 0, fmt.Errorf("read: %w", err)
	}
	if n > budget {
		return "", 0, ErrArchiveBomb
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
