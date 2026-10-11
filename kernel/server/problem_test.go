package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/clock"
)

func newProblems(t *testing.T) (*Problems, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	return &Problems{
		Log:     slog.New(slog.NewJSONHandler(&logs, nil)),
		TraceID: func(*http.Request) string { return "trace-1" },
	}, &logs
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
		t.Fatalf("Content-Type = %q, want %q", ct, api.ProblemContentType)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return m
}

// rule: ARC-I-010
func TestARC_I_010_ErrorsAndLogsCarryNoClientData(t *testing.T) {
	secrets := []string{"Jane Roe", "123456", "1,234.56", "sk-live-abc123"}
	tests := []struct {
		name string
		err  error
	}{
		{"wrapped db error", fmt.Errorf("insert receipt for Jane Roe amount 123456: %w", errors.New("pq: duplicate key"))},
		{"plain error with token", errors.New("auth failed for token sk-live-abc123")},
		{"amount in message", fmt.Errorf("receipt $1,234.56 for Jane Roe: %w", os.ErrDeadlineExceeded)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw, logs := newProblems(t)
			rec := httptest.NewRecorder()
			pw.WriteError(rec, httptest.NewRequest(http.MethodPost, "/v1/ledgers/0190-abc/receipts", nil), tt.err)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			body := decode(t, rec)
			if body["code"] != CodeInternal || !strings.Contains(fmt.Sprint(body["instance"]), "0190-abc") {
				t.Fatalf("body = %v, want code %s and an id-only instance", body, CodeInternal)
			}
			out := rec.Body.String() + logs.String()
			for _, s := range secrets {
				if strings.Contains(out, s) {
					t.Errorf("output leaks %q:\n%s", s, out)
				}
			}
		})
	}
}

// rule: ARC-I-009
func TestARC_I_009_ProblemJSONMembers(t *testing.T) {
	tests := []struct {
		name string
		body ProblemBody
		want map[string]any
	}{
		{
			name: "sod violation carries gate_id",
			body: ProblemBody{Problem: api.Problem{Title: "Preparer cannot approve", Status: 403, Code: "sod_violation", Rule: "GAT-I-001"}, GateID: "0190-gate"},
			want: map[string]any{"type": "urn:counsel:problem:sod_violation", "code": "sod_violation", "status": 403.0, "gate_id": "0190-gate", "trace_id": "trace-1", "instance": "/v1/bills/1/approve", "rule": "GAT-I-001"},
		},
		{
			name: "validation carries errors[]",
			body: ProblemBody{Problem: NewProblem(422, CodeValidationFailed, "Invalid body"), Errors: []FieldError{{Pointer: "/amount_cents", Code: "invalid_money"}}},
			want: map[string]any{"type": "urn:counsel:problem:validation_failed", "code": CodeValidationFailed, "status": 422.0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw, _ := newProblems(t)
			rec := httptest.NewRecorder()
			pw.WriteError(rec, httptest.NewRequest(http.MethodPost, "/v1/bills/1/approve", nil), &tt.body)
			if rec.Code != tt.body.Status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.body.Status)
			}
			got := decode(t, rec)
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
			if len(tt.body.Errors) > 0 {
				errs, _ := got["errors"].([]any)
				if len(errs) != 1 || errs[0].(map[string]any)["pointer"] != "/amount_cents" {
					t.Errorf("errors = %v", got["errors"])
				}
			}
		})
	}
}

func TestWriteErrorSentinels(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("list: %w", api.ErrBadCursor), 400, CodeBadCursor},
		{ErrBadDate, 400, CodeInvalidDate},
		{ErrPreconditionRequired, 428, CodePreconditionRequired},
		{fmt.Errorf("patch: %w", ErrPreconditionFailed), 412, CodeVersionConflict},
		{&api.Problem{Title: "Not found", Status: 404, Code: "not_found"}, 404, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			pw, _ := newProblems(t)
			rec := httptest.NewRecorder()
			pw.WriteError(rec, httptest.NewRequest(http.MethodGet, "/v1/x", nil), tt.err)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code || got["type"] != ProblemTypePrefix+tt.code {
				t.Fatalf("got %d %v, want %d %s", rec.Code, got, tt.status, tt.code)
			}
		})
	}
}

// rule: ARC-I-004
func TestARC_I_004_BusinessDates(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	t.Run("instant to business date", func(t *testing.T) {
		at, _ := time.Parse(time.RFC3339, "2026-03-31T23:30:00-04:00")
		if got, want := BusinessDateOf(at, toronto), (clock.Date{Year: 2026, Month: 3, Day: 31}); got != want {
			t.Fatalf("BusinessDateOf = %v, want %v", got, want)
		}
		if got := at.UTC().Format(time.RFC3339); got != "2026-04-01T03:30:00Z" {
			t.Fatalf("UTC = %s", got)
		}
	})
	tests := []struct {
		in   string
		want clock.Date
		err  error
	}{
		{"2026-03-31", clock.Date{Year: 2026, Month: 3, Day: 31}, nil},
		{"2026-02-30", clock.Date{}, ErrBadDate},
		{"2026-3-31", clock.Date{}, ErrBadDate},
		{"2026-03-31T00:00:00Z", clock.Date{}, ErrBadDate},
		{"", clock.Date{}, ErrBadDate},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseBusinessDate(tt.in)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("ParseBusinessDate(%q) = %v, %v; want %v, %v", tt.in, got, err, tt.want, tt.err)
			}
		})
	}
}

// rule: ARC-I-005
func TestARC_I_005_BadCursorIs400Problem(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"bare", api.ErrBadCursor},
		{"wrapped", fmt.Errorf("list bills: %w", api.ErrBadCursor)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw, _ := newProblems(t)
			rec := httptest.NewRecorder()
			pw.WriteError(rec, httptest.NewRequest(http.MethodGet, "/v1/bills?cursor=x", nil), tt.err)
			if got := decode(t, rec); rec.Code != 400 || got["code"] != CodeBadCursor {
				t.Fatalf("got %d %v", rec.Code, got)
			}
		})
	}
}

// rule: ARC-I-011
func TestARC_I_011_RateLimitedHeaders(t *testing.T) {
	tests := []struct {
		name      string
		rl        RateLimit
		wantRetry int
		policy    string
	}{
		{"money posts", RateLimit{Limit: 30, Window: time.Minute, RetryAfter: 1500 * time.Millisecond}, 2, "30;w=60"},
		{"sub-second rounds up to 1", RateLimit{Limit: 120, Window: time.Minute, RetryAfter: 0}, 1, "120;w=60"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pw, _ := newProblems(t)
			rec := httptest.NewRecorder()
			pw.WriteRateLimited(rec, httptest.NewRequest(http.MethodPost, "/v1/payments", nil), tt.rl)
			got := decode(t, rec)
			if rec.Code != 429 || got["code"] != CodeRateLimited {
				t.Fatalf("got %d %v", rec.Code, got)
			}
			if ra, _ := strconv.Atoi(rec.Header().Get("Retry-After")); ra != tt.wantRetry {
				t.Errorf("Retry-After = %d, want %d", ra, tt.wantRetry)
			}
			if p := rec.Header().Get("RateLimit-Policy"); p != tt.policy {
				t.Errorf("RateLimit-Policy = %q, want %q", p, tt.policy)
			}
			if rec.Header().Get("RateLimit") == "" {
				t.Error("RateLimit header missing")
			}
		})
	}
}

// rule: ARC-I-051
func TestARC_I_051_OperationsDocumentProblemResponses(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash("../../api/openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	const problemRef = "#/components/responses/Problem"
	var bad []string
	ops := 0
	for path, item := range doc.Paths {
		// The provider webhook receiver is session-less and documents 400/401/429 only;
		// the contract is frozen, so it is excepted pending Q-004.
		if path == "/hooks/{provider}" {
			continue
		}
		for method, node := range item {
			m := strings.ToUpper(method)
			if !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, m) {
				continue
			}
			ops++
			var op struct {
				Parameters []struct {
					Ref string `yaml:"$ref"`
				} `yaml:"parameters"`
				Responses map[string]struct {
					Ref string `yaml:"$ref"`
				} `yaml:"responses"`
			}
			if err := node.Decode(&op); err != nil {
				t.Fatalf("%s %s: %v", m, path, err)
			}
			ifMatch := slices.ContainsFunc(op.Parameters, func(p struct {
				Ref string `yaml:"$ref"`
			}) bool {
				return strings.HasSuffix(p.Ref, "/IfMatch")
			})
			for _, s := range ProblemStatuses(m, ifMatch) {
				if r, ok := op.Responses[strconv.Itoa(s)]; !ok || r.Ref != problemRef {
					bad = append(bad, fmt.Sprintf("%s %s: %d", m, path, s))
				}
			}
		}
	}
	if ops == 0 {
		t.Fatal("no operations found")
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("operations missing Problem responses:\n%s", strings.Join(bad, "\n"))
	}
}

func TestProblemStatuses(t *testing.T) {
	tests := []struct {
		method  string
		ifMatch bool
		want    int
	}{
		{http.MethodGet, false, 4},
		{http.MethodPost, false, 7},
		{http.MethodPatch, true, 9},
	}
	for _, tt := range tests {
		if got := len(ProblemStatuses(tt.method, tt.ifMatch)); got != tt.want {
			t.Errorf("%s ifMatch=%v: %d statuses, want %d", tt.method, tt.ifMatch, got, tt.want)
		}
	}
}
