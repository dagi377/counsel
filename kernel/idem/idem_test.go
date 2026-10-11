package idem_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
)

const key = "6f1c2a9e-0000-7000-8000-000000000001"

// effectHandler counts effects and answers with the request body echoed in a JSON object.
type effectHandler struct{ effects atomic.Int32 }

func (h *effectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := h.effects.Add(1)
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/v1/receipts/%d", n))
	w.WriteHeader(http.StatusCreated)
	_, _ = fmt.Fprintf(w, `{"id":%d,"amount_cents":%v}`, n, in["amount_cents"])
}

func principal(r *http.Request) string { return r.Header.Get("X-Test-User") }

func post(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/receipts", strings.NewReader(body))
	r.Header.Set("X-Test-User", "u1")
	if key != "" {
		r.Header.Set(idem.HeaderName, key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func problemCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var p api.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("problem body %q: %v", w.Body.String(), err)
	}
	// rule: ARC-I-009
	if want := api.ProblemTypePrefix + p.Code; p.Type != want {
		t.Fatalf("problem type = %q, want %q", p.Type, want)
	}
	return p.Code
}

// rule: ARC-I-008
func TestARC_I_008_ReplayAndMismatch(t *testing.T) {
	tests := []struct {
		name       string
		second     string
		wantStatus int
		wantCode   string
		wantSame   bool
		effects    int32
	}{
		{name: "same body replays byte-identical", second: `{"amount_cents":50000}`, wantStatus: http.StatusCreated, wantSame: true, effects: 1},
		{name: "different body is 422", second: `{"amount_cents":60000}`, wantStatus: http.StatusUnprocessableEntity, wantCode: idem.CodeKeyReused, effects: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &effectHandler{}
			mw := idem.New(idemfake.New(), principal, time.Hour)(h)
			first := post(t, mw, key, `{"amount_cents":50000}`)
			if first.Code != http.StatusCreated || first.Header().Get(idem.ReplayedHeader) != "" {
				t.Fatalf("first: %d %v", first.Code, first.Header())
			}
			second := post(t, mw, key, tt.second)
			if second.Code != tt.wantStatus {
				t.Fatalf("second status = %d, want %d", second.Code, tt.wantStatus)
			}
			if tt.wantCode != "" && problemCode(t, second) != tt.wantCode {
				t.Fatalf("code = %s, want %s", problemCode(t, second), tt.wantCode)
			}
			if tt.wantSame {
				if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
					t.Fatalf("replayed body %q != %q", second.Body.String(), first.Body.String())
				}
				if second.Header().Get("Location") != first.Header().Get("Location") || second.Header().Get(idem.ReplayedHeader) != "true" {
					t.Fatalf("replayed headers = %v", second.Header())
				}
			}
			if got := h.effects.Load(); got != tt.effects {
				t.Fatalf("effects = %d, want %d", got, tt.effects)
			}
		})
	}
}

// rule: ARC-I-007
func TestARC_I_007_KeyRequired(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "missing"},
		{name: "too short", key: "abc"},
		{name: "too long", key: strings.Repeat("k", idem.MaxKeyLen+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &effectHandler{}
			w := post(t, idem.New(idemfake.New(), principal, time.Hour)(h), tt.key, `{"amount_cents":1}`)
			if w.Code != http.StatusBadRequest || problemCode(t, w) != idem.CodeKeyRequired {
				t.Fatalf("got %d %s, want 400 %s", w.Code, w.Body.String(), idem.CodeKeyRequired)
			}
			if h.effects.Load() != 0 {
				t.Fatal("handler ran without a key")
			}
		})
	}
}

// blocking holds the first request until release is closed.
type blocking struct {
	started chan struct{}
	release chan struct{}
}

func (b *blocking) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	close(b.started)
	<-b.release
	w.WriteHeader(http.StatusCreated)
}

// rule: ARC-U-021
func TestARC_U_021_KeyHeldWhileHandlerRuns(t *testing.T) {
	tests := []struct {
		name       string
		wantStatus int
	}{
		{name: "concurrent duplicate is 409", wantStatus: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &blocking{started: make(chan struct{}), release: make(chan struct{})}
			mw := idem.New(idemfake.New(), principal, time.Hour)(b)
			done := make(chan struct{})
			go func() { post(t, mw, key, `{}`); close(done) }()
			<-b.started
			w := post(t, mw, key, `{}`)
			close(b.release)
			<-done
			if w.Code != tt.wantStatus || problemCode(t, w) != idem.CodeInProgress {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

// failing answers 503 the first time and 201 after.
type failing struct{ calls atomic.Int32 }

func (f *failing) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if f.calls.Add(1) == 1 {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func TestServerErrorReleasesKey(t *testing.T) {
	f := &failing{}
	mw := idem.New(idemfake.New(), principal, time.Hour)(f)
	if w := post(t, mw, key, `{}`); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("first = %d", w.Code)
	}
	if w := post(t, mw, key, `{}`); w.Code != http.StatusCreated || w.Header().Get(idem.ReplayedHeader) != "" {
		t.Fatalf("retry after 5xx = %d %v, want a fresh 201", w.Code, w.Header())
	}
}

// rule: ARC-U-025
// rule: ARC-I-031
func TestARC_U_025_NewKeyIsUUIDv7(t *testing.T) {
	uuid7 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	tests := []struct {
		name string
		at   time.Time
	}{
		{name: "epoch", at: time.UnixMilli(0)},
		{name: "now", at: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := idem.NewKey(tt.at, bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
			if err != nil {
				t.Fatal(err)
			}
			if !uuid7.MatchString(k) || len(k) < idem.MinKeyLen {
				t.Fatalf("key %q is not a UUIDv7", k)
			}
			later, _ := idem.NewKey(tt.at.Add(time.Millisecond), bytes.NewReader(make([]byte, 16)))
			if later[:13] <= k[:13] {
				t.Fatalf("keys do not sort by time: %s then %s", k, later)
			}
		})
	}
}

// TestARC_I_031_NewKeyIsUUIDv7 covers the envelope id format shared with NewKey.
func TestARC_I_031_NewKeyIsUUIDv7(t *testing.T) { TestARC_U_025_NewKeyIsUUIDv7(t) }

// rule: ARC-I-032
func TestARC_I_032_DropStaleSequence(t *testing.T) {
	tests := []struct {
		lastSeen, seq int64
		want          bool
	}{
		{lastSeen: 0, seq: 1, want: true},
		{lastSeen: 3, seq: 2, want: false},
		{lastSeen: 3, seq: 3, want: false},
		{lastSeen: 3, seq: 4, want: true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_after_%d", tt.seq, tt.lastSeen), func(t *testing.T) {
			if got := idem.Fresh(tt.lastSeen, tt.seq); got != tt.want {
				t.Fatalf("Fresh(%d, %d) = %v, want %v", tt.lastSeen, tt.seq, got, tt.want)
			}
		})
	}
}

// rule: ARC-U-023
func TestARC_U_023_BackoffAndRetryable(t *testing.T) {
	ceil := func(max int64) int64 { return max }
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 2 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 8 * time.Second},
		{attempt: 10, want: 15 * time.Minute},
		{attempt: 64, want: 15 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			if got := idem.External.Delay(tt.attempt, ceil); got != tt.want {
				t.Fatalf("Delay = %s, want %s", got, tt.want)
			}
			if got := idem.External.Delay(tt.attempt, nil); got < 0 || got > tt.want {
				t.Fatalf("jittered Delay %s outside [0, %s]", got, tt.want)
			}
		})
	}
	statuses := map[int]bool{500: true, 503: true, 408: true, 409: true, 425: true, 429: true, 400: false, 404: false, 422: false, 200: false}
	for s, want := range statuses {
		if got := idem.Retryable(s); got != want {
			t.Errorf("Retryable(%d) = %v, want %v", s, got, want)
		}
	}
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if a := idem.External.Next(20, 503, first, first.Add(time.Hour)); a != idem.DeadLetter {
		t.Fatalf("20th attempt = %v, want DeadLetter", a)
	}
	if a := idem.External.Next(3, 404, first, first); a != idem.DeadLetter {
		t.Fatalf("404 = %v, want DeadLetter", a)
	}
}

// rule: ARC-I-047
func TestARC_I_047_MoneyStopsAt20hThenLooksUp(t *testing.T) {
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		policy idem.Policy
		n      int
		status int
		age    time.Duration
		want   idem.Action
	}{
		{name: "money retries early", policy: idem.MoneyExternal, n: 3, status: 503, age: time.Hour, want: idem.Retry},
		{name: "money at 20h looks up", policy: idem.MoneyExternal, n: 5, status: 503, age: 20 * time.Hour, want: idem.Lookup},
		{name: "money attempts spent looks up", policy: idem.MoneyExternal, n: 20, status: 503, age: time.Hour, want: idem.Lookup},
		{name: "other at 20h still retries", policy: idem.External, n: 5, status: 503, age: 20 * time.Hour, want: idem.Retry},
		{name: "other at 24h dead-letters", policy: idem.External, n: 5, status: 503, age: 24 * time.Hour, want: idem.DeadLetter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Next(tt.n, tt.status, first, first.Add(tt.age)); got != tt.want {
				t.Fatalf("Next = %v, want %v", got, tt.want)
			}
		})
	}
}
