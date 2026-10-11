package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func newCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := NewCodec(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewCodecKeyLength(t *testing.T) {
	tests := []struct {
		name string
		key  []byte
		ok   bool
	}{
		{"short", []byte("short"), false},
		{"min", testKey, true},
	}
	for _, tt := range tests {
		if _, err := NewCodec(tt.key); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}
}

func TestCursorRoundTripAndTamper(t *testing.T) {
	c := newCodec(t)
	other, _ := NewCodec([]byte("ffffffffffffffffffffffffffffffff"))
	tests := []struct {
		name   string
		cursor Cursor
		mutate func(string) string
		want   error
	}{
		{"round trip", Cursor{After: "2026-03-31T12:00:00Z|0190-abc", Limit: 100}, nil, nil},
		{"empty", Cursor{}, nil, nil},
		{"body flipped", Cursor{After: "x", Limit: 2}, func(s string) string {
			if s[0] == 'A' {
				return "B" + s[1:]
			}
			return "A" + s[1:]
		}, ErrBadCursor},
		{"signature truncated", Cursor{After: "x"}, func(s string) string { return s[:len(s)-2] }, ErrBadCursor},
		{"no signature", Cursor{After: "x"}, func(s string) string { b, _, _ := strings.Cut(s, "."); return b }, ErrBadCursor},
		{"other key", Cursor{After: "x"}, func(string) string { s, _ := other.Encode(Cursor{After: "x"}); return s }, ErrBadCursor},
		{"garbage", Cursor{}, func(string) string { return "%%%.%%%" }, ErrBadCursor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := c.Encode(tt.cursor)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(s, tt.cursor.After) && tt.cursor.After != "" {
				t.Fatalf("cursor is not opaque: %s", s)
			}
			if tt.mutate != nil {
				s = tt.mutate(s)
			}
			got, err := c.Decode(s)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Decode err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && got != tt.cursor {
				t.Fatalf("Decode = %+v, want %+v", got, tt.cursor)
			}
		})
	}
}

func TestClampAtOpenAPIMaximum(t *testing.T) {
	c := newCodec(t)
	tests := []struct{ in, want int }{{-1, 50}, {0, 50}, {1, 1}, {199, 199}, {200, 200}, {201, 200}, {1 << 30, 200}}
	for _, tt := range tests {
		if got := c.Clamp(tt.in); got != tt.want {
			t.Errorf("Clamp(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// listHandler is a list endpoint that answers 400 problem+json for a bad cursor.
func listHandler(codec CursorCodec) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := FromRequest(r, codec)
		if err != nil {
			p := BadCursorProblem()
			if errors.Is(err, ErrBadLimit) {
				p = Problem{Title: "limit must be an integer", Status: 400, Code: "validation_failed"}
			}
			w.Header().Set("Content-Type", ProblemContentType)
			w.WriteHeader(p.Status)
			_ = json.NewEncoder(w).Encode(p)
			return
		}
		_ = json.NewEncoder(w).Encode(c)
	})
}

func TestFromRequest(t *testing.T) {
	c := newCodec(t)
	good, _ := c.Encode(Cursor{After: "k1", Limit: 20})
	tests := []struct {
		name   string
		query  url.Values
		status int
		code   string
		want   Cursor
	}{
		{"defaults", url.Values{}, 200, "", Cursor{Limit: 50}},
		{"limit capped", url.Values{"limit": {"500"}}, 200, "", Cursor{Limit: 200}},
		{"cursor keeps its limit", url.Values{"cursor": {good}}, 200, "", Cursor{After: "k1", Limit: 20}},
		{"explicit limit wins", url.Values{"cursor": {good}, "limit": {"7"}}, 200, "", Cursor{After: "k1", Limit: 7}},
		{"tampered cursor", url.Values{"cursor": {"x" + good}}, 400, CodeBadCursor, Cursor{}},
		{"bad limit", url.Values{"limit": {"ten"}}, 400, "validation_failed", Cursor{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			listHandler(c).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/bills?"+tt.query.Encode(), nil))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.status == 400 {
				var p Problem
				if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
					t.Fatalf("Content-Type = %q", ct)
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Code != tt.code {
					t.Fatalf("problem = %+v, %v; want code %s", p, err, tt.code)
				}
				return
			}
			var got Cursor
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != tt.want {
				t.Fatalf("cursor = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}
