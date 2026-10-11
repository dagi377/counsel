package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/server/middleware"
)

// minMoneyOps keeps the conformance test from passing on an empty or truncated contract.
const minMoneyOps = 20

// rule: ARC-I-007
func TestARC_I_007_MoneyOperationsReferenceIdempotencyKey(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	ops, err := middleware.IdempotentOperations(spec)
	if err != nil {
		t.Fatalf("IdempotentOperations: %v", err)
	}
	money := 0
	for _, op := range ops {
		if op.Money {
			money++
		}
		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			if !op.HasKeyParam {
				t.Errorf("%s %s (%s) does not reference %s", op.Method, op.Path, op.OperationID, middleware.IdempotencyKeyRef)
			}
		})
	}
	if money < minMoneyOps {
		t.Fatalf("count(x-money) = %d, want >= %d", money, minMoneyOps)
	}
}

// rule: ARC-I-007
func TestARC_I_007_IdempotentOperationsParse(t *testing.T) {
	const spec = `
openapi: 3.1.0
paths:
  /a:
    parameters:
    - {$ref: '#/components/parameters/IdempotencyKey'}
    post:
      operationId: shared
      x-money: true
  /b:
    post:
      operationId: missing
      x-external-effect: true
    get:
      operationId: read
  /c:
    post:
      operationId: own
      x-money: true
      parameters:
      - {$ref: '#/components/parameters/IdempotencyKey'}
`
	ops, err := middleware.IdempotentOperations([]byte(spec))
	if err != nil {
		t.Fatalf("IdempotentOperations: %v", err)
	}
	want := []middleware.IdempotentOperation{
		{Method: "POST", Path: "/a", OperationID: "shared", Money: true, HasKeyParam: true},
		{Method: "POST", Path: "/b", OperationID: "missing", ExternalEffect: true},
		{Method: "POST", Path: "/c", OperationID: "own", Money: true, HasKeyParam: true},
	}
	if len(ops) != len(want) {
		t.Fatalf("got %d ops %+v, want %d", len(ops), ops, len(want))
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Errorf("op %d = %+v, want %+v", i, ops[i], want[i])
		}
	}
	if _, err := middleware.IdempotentOperations([]byte("paths: [")); err == nil {
		t.Error("malformed yaml: want error")
	}
}

// rule: ARC-I-007
func TestARC_I_007_MoneyPostWithoutKeyIs400(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		key      string
		wantCode int
		wantNext bool
	}{
		{"post without key", http.MethodPost, "", http.StatusBadRequest, false},
		{"post with blank key", http.MethodPost, "   ", http.StatusBadRequest, false},
		{"post with key", http.MethodPost, "0b8f6c1e-1d1a-4a4f-9d55-3a0f4b8c2e11", http.StatusNoContent, true},
		{"get without key", http.MethodGet, "", http.StatusNoContent, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			h := middleware.RequireIdempotencyKey()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(tt.method, "/bills", nil)
			if tt.key != "" {
				req.Header.Set(idem.HeaderName, tt.key)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode || called != tt.wantNext {
				t.Fatalf("status %d next %v, want %d next %v", rec.Code, called, tt.wantCode, tt.wantNext)
			}
			if tt.wantNext {
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
				t.Errorf("content-type %q, want %q", ct, api.ProblemContentType)
			}
			var p api.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Code != idem.CodeKeyRequired || p.Rule != "ARC-I-007" || p.Status != http.StatusBadRequest {
				t.Errorf("problem = %+v", p)
			}
		})
	}
}
