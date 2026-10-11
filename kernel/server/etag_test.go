package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/server/serverfake"
)

// patchHandler serves PATCH on a resource at version 3 and bumps it on success.
func patchHandler(pw server.ProblemWriter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const current = 3
		// rule: ARC-I-006
		switch err := server.CheckIfMatch(r, current); {
		case errors.Is(err, server.ErrPreconditionRequired):
			pw.WriteProblem(w, r, api.Problem{Title: "If-Match required", Status: http.StatusPreconditionRequired, Code: "precondition_required"})
		case errors.Is(err, server.ErrPreconditionFailed):
			pw.WriteProblem(w, r, api.Problem{Title: "Version conflict", Status: http.StatusPreconditionFailed, Code: "version_conflict"})
		default:
			server.SetETag(w, current+1)
			w.WriteHeader(http.StatusOK)
		}
	})
}

// rule: ARC-I-006
func TestARC_I_006_IfMatchRequiredAndStrong(t *testing.T) {
	h := patchHandler(serverfake.New())
	tests := []struct {
		name     string
		ifMatch  []string
		status   int
		wantETag string
	}{
		{"missing", nil, http.StatusPreconditionRequired, ""},
		{"empty", []string{""}, http.StatusPreconditionRequired, ""},
		{"stale", []string{`"2"`}, http.StatusPreconditionFailed, ""},
		{"weak current", []string{`W/"3"`}, http.StatusPreconditionFailed, ""},
		{"unquoted", []string{`3`}, http.StatusPreconditionFailed, ""},
		{"current", []string{`"3"`}, http.StatusOK, `"4"`},
		{"list with current", []string{`"1", "3"`}, http.StatusOK, `"4"`},
		{"repeated header", []string{`"1"`, `"3"`}, http.StatusOK, `"4"`},
		{"star", []string{`*`}, http.StatusOK, `"4"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/v1/bills/1", nil)
			for _, v := range tt.ifMatch {
				req.Header.Add("If-Match", v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.status >= 400 && rec.Header().Get("Content-Type") != api.ProblemContentType {
				t.Errorf("Content-Type = %q, want problem+json", rec.Header().Get("Content-Type"))
			}
			if got := rec.Header().Get("ETag"); got != tt.wantETag {
				t.Errorf("ETag = %q, want %q", got, tt.wantETag)
			}
		})
	}
}

func TestETag(t *testing.T) {
	tests := []struct {
		v    int64
		want string
	}{{0, `"0"`}, {3, `"3"`}, {1 << 40, `"1099511627776"`}}
	for _, tt := range tests {
		if got := server.ETag(tt.v); got != tt.want {
			t.Errorf("ETag(%d) = %s, want %s", tt.v, got, tt.want)
		}
	}
}
