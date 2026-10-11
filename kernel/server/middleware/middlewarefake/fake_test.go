package middlewarefake

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dagi377/counsel/kernel/server/middleware"
)

func TestAuthenticator(t *testing.T) {
	alice := middleware.Principal{ID: "u1", Kind: middleware.PrincipalUser}
	agent := middleware.Principal{ID: "run1", Kind: middleware.PrincipalAgent, ActingFor: "u1"}
	a := NewAuthenticator(map[string]middleware.Principal{"t-alice": alice, "t-agent": agent})
	tests := []struct {
		name    string
		header  string
		want    middleware.Principal
		wantErr error
	}{
		{name: "user", header: "Bearer t-alice", want: alice},
		{name: "agent", header: "Bearer t-agent", want: agent},
		{name: "missing", wantErr: middleware.ErrUnauthenticated},
		{name: "wrong scheme", header: "Basic t-alice", wantErr: middleware.ErrUnauthenticated},
		{name: "unknown token", header: "Bearer nope", wantErr: middleware.ErrUnauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set(middleware.HeaderAuthorization, tt.header)
			}
			got, err := a.Authenticate(r)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("Authenticate = %+v, %v; want %+v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestProblemWriter(t *testing.T) {
	tests := []struct {
		name      string
		in        middleware.Problem
		reqID     string
		wantTitle string
		wantRule  string
	}{
		{name: "defaults title", in: middleware.Problem{Status: 404}, wantTitle: "Not Found"},
		{name: "keeps rule", in: middleware.Problem{Status: 422, Title: "Bad", Rule: "BIL-C-002"}, wantTitle: "Bad", wantRule: "BIL-C-002"},
		{name: "copies request id", in: middleware.Problem{Status: 409}, reqID: "req-9", wantTitle: "Conflict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw := &ProblemWriter{}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			if tt.reqID != "" {
				r.Header.Set(middleware.HeaderRequestID, tt.reqID)
			}
			pw.WriteProblem(w, r, tt.in)
			if w.Code != tt.in.Status {
				t.Fatalf("status = %d, want %d", w.Code, tt.in.Status)
			}
			if ct := w.Header().Get("Content-Type"); ct != middleware.ProblemContentType {
				t.Fatalf("content-type = %q", ct)
			}
			var got middleware.Problem
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Title != tt.wantTitle || got.Rule != tt.wantRule || got.RequestID != tt.reqID || got.Type != "about:blank" {
				t.Fatalf("body = %+v", got)
			}
			if len(pw.Written) != 1 {
				t.Fatalf("written = %d", len(pw.Written))
			}
		})
	}
}

func TestRequestIDs(t *testing.T) {
	g := &RequestIDs{}
	for _, want := range []string{"req-1", "req-2", "req-3"} {
		if got := g.NewRequestID(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
