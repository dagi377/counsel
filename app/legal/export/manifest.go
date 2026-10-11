package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrMismatch is wrapped by Verify for every file or count that differs from the manifest.
var ErrMismatch = errors.New("export: manifest mismatch")

// Firm identifies the firm an export belongs to.
type Firm struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// File is one manifest entry.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Entity string `json:"entity,omitempty"`
	Rows   *int64 `json:"rows,omitempty"`
	Schema string `json:"schema,omitempty"`
}

// TrustBalance is the total held in one trust account at as_of.
type TrustBalance struct {
	AccountID                string  `json:"account_id"`
	TotalCents               int64   `json:"total_cents"`
	Currency                 string  `json:"currency"`
	LastSignedReconciliation *string `json:"last_signed_reconciliation"`
}

// AuditChain summarises the exported audit hash chain.
type AuditChain struct {
	Events    int64  `json:"events"`
	HeadHash  string `json:"head_hash"`
	Algorithm string `json:"algorithm"`
}

// LegacySource points at an imported legacy archive.
type LegacySource struct {
	System string `json:"system"`
	Region string `json:"region"`
	Path   string `json:"path"`
}

// Manifest is manifest.json of an export (export_manifest_schema, counsel-export/1).
type Manifest struct {
	Format        string         `json:"format"`
	Firm          Firm           `json:"firm"`
	GeneratedAt   string         `json:"generated_at"`
	AsOf          string         `json:"as_of"`
	RequestedBy   string         `json:"requested_by,omitempty"`
	ApprovedBy    string         `json:"approved_by,omitempty"`
	Files         []File         `json:"files"`
	TrustBalances []TrustBalance `json:"trust_balances"`
	AuditChain    AuditChain     `json:"audit_chain"`
	Legacy        []LegacySource `json:"legacy_sources,omitempty"`
}

// Content is one file to export. Rows is set for entity files and counts data lines.
type Content struct {
	Path   string
	Data   []byte
	Entity string
	Rows   *int64
}

// Header is the manifest data that does not come from the files themselves.
type Header struct {
	Firm          Firm
	GeneratedAt   time.Time
	AsOf          time.Time
	RequestedBy   string
	ApprovedBy    string
	TrustBalances []TrustBalance
	AuditChain    AuditChain
	Legacy        []LegacySource
}

// BuildManifest lists every file with its sha256, size and row count, sorted by path.
// manifest.json and its signature are never listed.
func BuildManifest(h Header, files []Content) Manifest {
	m := Manifest{
		Format:        Format,
		Firm:          h.Firm,
		GeneratedAt:   Instant(h.GeneratedAt),
		AsOf:          Instant(h.AsOf),
		RequestedBy:   h.RequestedBy,
		ApprovedBy:    h.ApprovedBy,
		Files:         []File{},
		TrustBalances: []TrustBalance{},
		AuditChain:    h.AuditChain,
		Legacy:        h.Legacy,
	}
	m.AuditChain.Algorithm = "sha256"
	for _, b := range h.TrustBalances {
		b.Currency = Currency
		m.TrustBalances = append(m.TrustBalances, b)
	}
	for _, f := range files {
		if f.Path == ManifestPath || f.Path == SignaturePath {
			continue
		}
		// rule: MIG-071
		sum := sha256.Sum256(f.Data)
		e := File{Path: f.Path, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(f.Data)), Entity: f.Entity, Rows: f.Rows}
		if f.Entity != "" {
			e.Schema = "schema/" + f.Entity + ".json"
		}
		m.Files = append(m.Files, e)
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	return m
}

// Marshal encodes the manifest as indented JSON.
func (m Manifest) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("export manifest: %w", err)
	}
	return b, nil
}

// Verify checks files against m: every listed path present with the same sha256 and size,
// no unlisted file, and each entity's row count equal to liveRows (the DB count at export
// time). A nil liveRows skips the count check. All differences are joined in one error.
func Verify(m Manifest, files map[string][]byte, liveRows map[string]int64) error {
	var errs []error
	listed := make(map[string]bool, len(m.Files))
	for _, f := range m.Files {
		listed[f.Path] = true
		data, ok := files[f.Path]
		if !ok {
			errs = append(errs, fmt.Errorf("%w: %s missing", ErrMismatch, f.Path))
			continue
		}
		// rule: MIG-071
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != f.SHA256 || int64(len(data)) != f.Bytes {
			errs = append(errs, fmt.Errorf("%w: %s sha256", ErrMismatch, f.Path))
		}
		if liveRows != nil && f.Entity != "" && f.Rows != nil {
			if live, ok := liveRows[f.Entity]; !ok || live != *f.Rows {
				errs = append(errs, fmt.Errorf("%w: %s rows %d, live %d", ErrMismatch, f.Path, *f.Rows, live))
			}
		}
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if !listed[p] && p != ManifestPath && p != SignaturePath {
			errs = append(errs, fmt.Errorf("%w: %s not in manifest", ErrMismatch, p))
		}
	}
	return errors.Join(errs...)
}
