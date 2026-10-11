package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Page size bounds from the OpenAPI Limit parameter (minimum 1, maximum 200, default 50).
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// MinCursorKeyLen is the shortest HMAC key NewCodec accepts.
const MinCursorKeyLen = 32

// CodeBadCursor is the problem code for a malformed or tampered cursor.
const CodeBadCursor = "bad_cursor"

// ErrBadLimit is returned when ?limit is not an integer (HTTP 400).
var ErrBadLimit = errors.New("api: limit must be an integer")

// Codec is the production CursorCodec: base64url(JSON) "." base64url(HMAC-SHA256).
type Codec struct {
	key []byte
}

var _ CursorCodec = (*Codec)(nil)

// NewCodec returns a Codec signing with key, which must be at least MinCursorKeyLen bytes.
func NewCodec(key []byte) (*Codec, error) {
	if len(key) < MinCursorKeyLen {
		return nil, fmt.Errorf("new cursor codec: key is %d bytes, want >= %d", len(key), MinCursorKeyLen)
	}
	return &Codec{key: append([]byte(nil), key...)}, nil
}

// Encode returns the opaque, signed form of c.
func (k *Codec) Encode(c Cursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	return body + "." + base64.RawURLEncoding.EncodeToString(k.mac(body)), nil
}

// Decode verifies s and parses it. Any malformed or altered s returns ErrBadCursor.
func (k *Codec) Decode(s string) (Cursor, error) {
	body, sig, ok := strings.Cut(s, ".")
	if !ok {
		return Cursor{}, ErrBadCursor
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, k.mac(body)) {
		return Cursor{}, ErrBadCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return Cursor{}, ErrBadCursor
	}
	return c, nil
}

// Clamp caps limit at MaxLimit; limit <= 0 gives DefaultLimit.
func (k *Codec) Clamp(limit int) int {
	switch {
	case limit <= 0:
		return DefaultLimit
	case limit > MaxLimit:
		return MaxLimit
	}
	return limit
}

// FromRequest reads ?cursor and ?limit. An explicit ?limit wins over the cursor's; the
// result is always clamped. A bad cursor returns ErrBadCursor, a non-integer limit ErrBadLimit.
func FromRequest(r *http.Request, codec CursorCodec) (Cursor, error) {
	q := r.URL.Query()
	var c Cursor
	if s := q.Get("cursor"); s != "" {
		var err error
		if c, err = codec.Decode(s); err != nil {
			return Cursor{}, err
		}
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return Cursor{}, ErrBadLimit
		}
		c.Limit = n
	}
	c.Limit = codec.Clamp(c.Limit)
	return c, nil
}

// BadCursorProblem is the 400 problem for ErrBadCursor.
func BadCursorProblem() Problem {
	return Problem{
		Type:   "urn:counsel:problem:" + CodeBadCursor,
		Title:  "Cursor is malformed or was altered",
		Status: http.StatusBadRequest,
		Code:   CodeBadCursor,
	}
}

func (k *Codec) mac(body string) []byte {
	m := hmac.New(sha256.New, k.key)
	m.Write([]byte(body))
	return m.Sum(nil)
}
