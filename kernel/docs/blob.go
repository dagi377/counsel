package docs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// MaxSignedURLTTL is the longest a signed document URL may live (ARC-U-012).
const MaxSignedURLTTL = 15 * time.Minute

// ErrSignedURLTTL means a signed URL was requested for longer than MaxSignedURLTTL.
var ErrSignedURLTTL = errors.New("docs: signed URL lifetime above 15 minutes")

// Bucket is the cell document bucket (versioned, encrypted with the cell KMS key, public
// access blocked). Deployments supply it; tests use an in-memory one.
type Bucket interface {
	PutObject(ctx context.Context, key string, r io.Reader, size int64) error
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)
	SignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// BlobRef is what the database and audit detail hold for stored bytes: a key, a digest
// and a size. It never carries document text.
type BlobRef struct {
	Key    string
	SHA256 string
	Size   int64
}

// AuditDetail returns the fields of a blob safe for logs and audit detail.
func (b BlobRef) AuditDetail() map[string]any {
	return map[string]any{"blob_key": b.Key, "sha256": b.SHA256, "size": b.Size}
}

// Blobs stores document bytes in the cell bucket only.
type Blobs struct {
	bucket Bucket
	log    *slog.Logger
}

// NewBlobs returns a blob store over the cell bucket. A nil logger uses slog.Default.
func NewBlobs(b Bucket, log *slog.Logger) *Blobs {
	if log == nil {
		log = slog.Default()
	}
	return &Blobs{bucket: b, log: log}
}

// Put writes r to the bucket under a content-addressed key and returns its reference.
// Neither the key, the log line nor any error carries the bytes.
//
// rule: ARC-U-012
func (s *Blobs) Put(ctx context.Context, r io.Reader) (BlobRef, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return BlobRef{}, fmt.Errorf("blob put: read: %w", err)
	}
	sum := sha256.Sum256(b)
	ref := BlobRef{Key: "docs/sha256/" + hex.EncodeToString(sum[:]), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))}
	if err := s.bucket.PutObject(ctx, ref.Key, bytes.NewReader(b), ref.Size); err != nil {
		return BlobRef{}, fmt.Errorf("blob put %s: %w", ref.Key, err)
	}
	s.log.InfoContext(ctx, "docs blob stored", "blob_key", ref.Key, "size", ref.Size)
	return ref, nil
}

// Open reads stored bytes back and checks them against the reference digest.
func (s *Blobs) Open(ctx context.Context, ref BlobRef) ([]byte, error) {
	rc, err := s.bucket.GetObject(ctx, ref.Key)
	if err != nil {
		return nil, fmt.Errorf("blob open %s: %w", ref.Key, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("blob open %s: read: %w", ref.Key, err)
	}
	if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, fmt.Errorf("blob open %s: digest mismatch: %w", ref.Key, ErrNotFound)
	}
	return b, nil
}

// SignedURL returns a short-lived signed GET URL. The caller has already passed the
// authz check; ttl must be positive and at most MaxSignedURLTTL.
//
// rule: ARC-U-012
func (s *Blobs) SignedURL(ctx context.Context, ref BlobRef, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > MaxSignedURLTTL {
		return "", fmt.Errorf("blob sign %s ttl %s: %w", ref.Key, ttl, ErrSignedURLTTL)
	}
	u, err := s.bucket.SignGet(ctx, ref.Key, ttl)
	if err != nil {
		return "", fmt.Errorf("blob sign %s: %w", ref.Key, err)
	}
	return u, nil
}
