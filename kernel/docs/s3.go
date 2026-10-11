package docs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrBucketConfig means an S3Bucket is missing its endpoint, region or credentials.
var ErrBucketConfig = errors.New("docs: cell bucket not configured")

// S3Bucket is the cell bucket over the S3 API, signed with AWS Signature Version 4. Endpoint
// is the bucket's path-style URL (https://s3.<region>.amazonaws.com/<bucket>). Objects are
// written with server-side KMS encryption; versioning, public-access blocking and replication
// are bucket settings the deployment owns (ARC-U-012). Credentials never reach logs or errors.
type S3Bucket struct {
	Endpoint     string
	Region       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	// KMSKeyID is the cell KMS key; empty uses the bucket's default KMS key.
	KMSKeyID string
	Client   *http.Client
	Now      func() time.Time
}

var _ Bucket = (*S3Bucket)(nil)

func (b *S3Bucket) ready() error {
	if b == nil || b.Endpoint == "" || b.Region == "" || b.AccessKey == "" || b.SecretKey == "" {
		return ErrBucketConfig
	}
	return nil
}

func (b *S3Bucket) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

func (b *S3Bucket) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// objectURL is the object's URL; each key segment is escaped.
func (b *S3Bucket) objectURL(key string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(b.Endpoint, "/"))
	if err != nil || u.Host == "" {
		return nil, ErrBucketConfig
	}
	segs := strings.Split(key, "/")
	for i, s := range segs {
		segs[i] = s3Escape(s)
	}
	u.RawPath = u.EscapedPath() + "/" + strings.Join(segs, "/")
	u.Path, _ = url.PathUnescape(u.RawPath)
	return u, nil
}

// s3Escape is the RFC 3986 encoding SigV4 canonicalises with.
func s3Escape(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			sb.WriteByte(c)
			continue
		}
		fmt.Fprintf(&sb, "%%%02X", c)
	}
	return sb.String()
}

func hmacSHA256(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// signature signs canonical for the day of t.
func (b *S3Bucket) signature(t time.Time, canonical string) string {
	day := t.Format("20060102")
	scope := day + "/" + b.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + t.Format("20060102T150405Z") + "\n" + scope + "\n" + sha256Hex([]byte(canonical))
	k := hmacSHA256([]byte("AWS4"+b.SecretKey), day)
	k = hmacSHA256(k, b.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	return hex.EncodeToString(hmacSHA256(k, toSign))
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, s3Escape(k)+"="+s3Escape(v))
		}
	}
	return strings.Join(parts, "&")
}

// sign adds the SigV4 Authorization header to req for a body with digest payload.
func (b *S3Bucket) sign(req *http.Request, payload string) {
	t := b.now()
	req.Header.Set("X-Amz-Date", t.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", payload)
	if b.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", b.SessionToken)
	}
	names := []string{"host"}
	for k := range req.Header {
		if lk := strings.ToLower(k); strings.HasPrefix(lk, "x-amz-") || lk == "content-type" {
			names = append(names, lk)
		}
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, n := range names {
		v := req.URL.Host
		if n != "host" {
			v = strings.TrimSpace(req.Header.Get(n))
		}
		ch.WriteString(n + ":" + v + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), canonicalQuery(req.URL.Query()),
		ch.String(), signed, payload}, "\n")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+b.AccessKey+"/"+t.Format("20060102")+"/"+b.Region+
		"/s3/aws4_request, SignedHeaders="+signed+", Signature="+b.signature(t, canonical))
}

func (b *S3Bucket) do(req *http.Request, op, key string) (*http.Response, error) {
	resp, err := b.client().Do(req)
	if err != nil {
		// The transport error names the host, never the credentials.
		return nil, fmt.Errorf("s3 %s %s: %w", op, key, err)
	}
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("s3 %s %s: %w", op, key, ErrNotFound)
		}
		return nil, fmt.Errorf("s3 %s %s: status %d", op, key, resp.StatusCode)
	}
	return resp, nil
}

// PutObject writes r under key, encrypted with the cell KMS key.
func (b *S3Bucket) PutObject(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := b.ready(); err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return fmt.Errorf("s3 put %s: read: %w", key, err)
	}
	if int64(len(body)) != size {
		return fmt.Errorf("s3 put %s: size mismatch", key)
	}
	u, err := b.objectURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	// rule: ARC-U-012
	req.Header.Set("X-Amz-Server-Side-Encryption", "aws:kms")
	if b.KMSKeyID != "" {
		req.Header.Set("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id", b.KMSKeyID)
	}
	b.sign(req, sha256Hex(body))
	resp, err := b.do(req, "put", key)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// GetObject reads the object under key.
func (b *S3Bucket) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := b.ready(); err != nil {
		return nil, err
	}
	u, err := b.objectURL(key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	b.sign(req, sha256Hex(nil))
	resp, err := b.do(req, "get", key)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// SignGet returns a presigned GET URL for key that expires after ttl.
func (b *S3Bucket) SignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	if err := b.ready(); err != nil {
		return "", err
	}
	if ttl <= 0 || ttl > MaxSignedURLTTL {
		return "", ErrSignedURLTTL
	}
	u, err := b.objectURL(key)
	if err != nil {
		return "", err
	}
	t := b.now()
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", b.AccessKey+"/"+t.Format("20060102")+"/"+b.Region+"/s3/aws4_request")
	q.Set("X-Amz-Date", t.Format("20060102T150405Z"))
	q.Set("X-Amz-Expires", fmt.Sprint(int(ttl/time.Second)))
	q.Set("X-Amz-SignedHeaders", "host")
	if b.SessionToken != "" {
		q.Set("X-Amz-Security-Token", b.SessionToken)
	}
	canonical := strings.Join([]string{http.MethodGet, u.EscapedPath(), canonicalQuery(q),
		"host:" + u.Host + "\n", "host", "UNSIGNED-PAYLOAD"}, "\n")
	u.RawQuery = canonicalQuery(q) + "&X-Amz-Signature=" + b.signature(t, canonical)
	return u.String(), nil
}
