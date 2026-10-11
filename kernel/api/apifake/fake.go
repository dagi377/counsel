// Package apifake is an in-memory api.CursorCodec with a fixed key.
package apifake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/kernel/api"
)

var _ api.CursorCodec = (*Fake)(nil)

// Fake signs cursors with HMAC-SHA256 over a fixed test key.
type Fake struct {
	Key          []byte
	DefaultLimit int
	MaxLimit     int
}

// New returns a Fake with the given default and maximum page sizes.
func New(defaultLimit, maxLimit int) *Fake {
	return &Fake{Key: []byte("apifake-test-key"), DefaultLimit: defaultLimit, MaxLimit: maxLimit}
}

// Encode returns base64url(json).base64url(mac).
func (f *Fake) Encode(c api.Cursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	return body + "." + f.sign(body), nil
}

// Decode verifies the MAC and parses the cursor.
func (f *Fake) Decode(s string) (api.Cursor, error) {
	body, sig, ok := strings.Cut(s, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(f.sign(body))) {
		return api.Cursor{}, api.ErrBadCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return api.Cursor{}, api.ErrBadCursor
	}
	var c api.Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return api.Cursor{}, api.ErrBadCursor
	}
	return c, nil
}

// Clamp caps limit at MaxLimit and maps limit <= 0 to DefaultLimit.
func (f *Fake) Clamp(limit int) int {
	switch {
	case limit <= 0:
		return f.DefaultLimit
	case limit > f.MaxLimit:
		return f.MaxLimit
	}
	return limit
}

func (f *Fake) sign(body string) string {
	m := hmac.New(sha256.New, f.Key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
