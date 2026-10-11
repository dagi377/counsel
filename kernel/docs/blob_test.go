package docs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type memBucket struct {
	objs map[string][]byte
	fail error
}

func (m *memBucket) PutObject(_ context.Context, key string, r io.Reader, _ int64) error {
	if m.fail != nil {
		return m.fail
	}
	b, err := io.ReadAll(r)
	m.objs[key] = b
	return err
}

func (m *memBucket) GetObject(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.objs[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memBucket) SignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	return fmt.Sprintf("https://bucket.invalid/%s?ttl=%d", key, int(ttl.Seconds())), nil
}

const secretText = "PRIVILEGED settlement offer 41,000"

// rule: ARC-U-012
func TestBlobsPutKeepsTextOutOfLogsAndAudit(t *testing.T) {
	tests := []struct {
		name    string
		fail    error
		wantErr bool
	}{
		{"stored", nil, false},
		{"bucket error", errors.New("bucket unavailable"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			bk := &memBucket{objs: map[string][]byte{}, fail: tc.fail}
			s := NewBlobs(bk, slog.New(slog.NewJSONHandler(&logs, nil)))
			ref, err := s.Put(context.Background(), strings.NewReader(secretText))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Put err = %v, wantErr %v", err, tc.wantErr)
			}
			surface := logs.String() + fmt.Sprint(ref.AuditDetail(), ref)
			if err != nil {
				surface += err.Error()
			}
			if strings.Contains(surface, "PRIVILEGED") || strings.Contains(surface, "41,000") {
				t.Errorf("document text leaked: %s", surface)
			}
			if tc.wantErr {
				return
			}
			if got := string(bk.objs[ref.Key]); got != secretText {
				t.Errorf("bucket holds %q, want document bytes", got)
			}
			b, err := s.Open(context.Background(), ref)
			if err != nil || string(b) != secretText {
				t.Errorf("Open = %q, %v", b, err)
			}
			bk.objs[ref.Key] = []byte("tampered")
			if _, err := s.Open(context.Background(), ref); !errors.Is(err, ErrNotFound) {
				t.Errorf("Open tampered err = %v, want ErrNotFound", err)
			}
		})
	}
}

// rule: ARC-U-012
func TestBlobsSignedURLTTL(t *testing.T) {
	s := NewBlobs(&memBucket{objs: map[string][]byte{}}, nil)
	ref := BlobRef{Key: "docs/sha256/ab"}
	tests := []struct {
		ttl     time.Duration
		wantErr bool
	}{
		{time.Minute, false},
		{MaxSignedURLTTL, false},
		{16 * time.Minute, true},
		{0, true},
		{-time.Second, true},
	}
	for _, tc := range tests {
		_, err := s.SignedURL(context.Background(), ref, tc.ttl)
		if tc.wantErr != errors.Is(err, ErrSignedURLTTL) {
			t.Errorf("SignedURL(%s) err = %v, wantErr %v", tc.ttl, err, tc.wantErr)
		}
	}
}
