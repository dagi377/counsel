package docs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/docs"
)

// TestS3Bucket: objects go up signed and KMS-encrypted and come back; a signed URL is short
// lived; a bucket without its settings refuses; errors never carry the secret.
//
// rule: ARC-U-012
func TestS3Bucket(t *testing.T) {
	objects := map[string][]byte{}
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			objects[r.URL.Path] = b
		case http.MethodGet:
			b, ok := objects[r.URL.Path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(b)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	now := func() time.Time { return time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC) }
	b := &docs.S3Bucket{Endpoint: srv.URL + "/cell-docs", Region: "ca-central-1", AccessKey: "AKID", SecretKey: "top-secret",
		KMSKeyID: "cell-key", Client: srv.Client(), Now: now}
	ctx := context.Background()
	if err := b.PutObject(ctx, "docs/sha256/ab", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if string(objects["/cell-docs/docs/sha256/ab"]) != "hello" || headers.Get("X-Amz-Server-Side-Encryption") != "aws:kms" ||
		!strings.HasPrefix(headers.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKID/20261011/ca-central-1/s3/aws4_request") ||
		strings.Contains(headers.Get("Authorization"), "top-secret") {
		t.Fatalf("put headers = %v", headers)
	}
	rc, err := b.GetObject(ctx, "docs/sha256/ab")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("get = %q", got)
	}
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"missing object", func() error { _, err := b.GetObject(ctx, "docs/none"); return err }, docs.ErrNotFound},
		{"size mismatch", func() error { return b.PutObject(ctx, "k", strings.NewReader("hello"), 3) }, nil},
		{"unconfigured", func() error { return (&docs.S3Bucket{}).PutObject(ctx, "k", strings.NewReader("x"), 1) }, docs.ErrBucketConfig},
		{"long signed url", func() error { _, err := b.SignGet(ctx, "k", 16*time.Minute); return err }, docs.ErrSignedURLTTL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || strings.Contains(err.Error(), "top-secret") {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	// A delete removes the object and a repeated delete succeeds.
	for i := range 2 {
		if err := b.DeleteObject(ctx, "docs/sha256/ab"); err != nil {
			t.Fatalf("delete %d: %v", i+1, err)
		}
		if _, ok := objects["/cell-docs/docs/sha256/ab"]; ok || !strings.HasPrefix(headers.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Fatalf("delete %d left the object or was unsigned", i+1)
		}
	}
	if err := (&docs.S3Bucket{}).DeleteObject(ctx, "k"); !errors.Is(err, docs.ErrBucketConfig) {
		t.Fatalf("unconfigured delete = %v", err)
	}
	u, err := b.SignGet(ctx, "docs/sha256/ab", 15*time.Minute)
	if err != nil || !strings.Contains(u, "X-Amz-Expires=900") || !strings.Contains(u, "X-Amz-Signature=") ||
		strings.Contains(u, "top-secret") {
		t.Fatalf("signed url = %q, %v", u, err)
	}
}
