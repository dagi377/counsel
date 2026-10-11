package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusHandler(t *testing.T) {
	const secret = "customer Jane Doe ticket 2026-0042 balance 4875000"
	tests := []struct {
		name   string
		checks map[string]CheckFunc
		want   string
		each   map[string]string
	}{
		{
			name:   "all pass",
			checks: map[string]CheckFunc{"gl": func(context.Context) error { return nil }, "billing": func(context.Context) error { return nil }},
			want:   StatusOK,
			each:   map[string]string{"gl": StatusOK, "billing": StatusOK},
		},
		{
			name: "one fails",
			checks: map[string]CheckFunc{
				"gl":      func(context.Context) error { return nil },
				"billing": func(context.Context) error { return errors.New(secret) },
			},
			want: StatusDegraded,
			each: map[string]string{"gl": StatusOK, "billing": StatusDegraded},
		},
		{
			name:   "panic",
			checks: map[string]CheckFunc{"docs": func(context.Context) error { panic(secret) }},
			want:   StatusDegraded,
			each:   map[string]string{"docs": StatusDegraded},
		},
		{
			name:   "no checks",
			checks: map[string]CheckFunc{},
			want:   StatusOK,
			each:   map[string]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sc := NewSmokeChecks(time.Second)
			for n, fn := range tc.checks {
				if err := sc.Add(n, fn); err != nil {
					t.Fatal(err)
				}
			}
			rec := httptest.NewRecorder()
			StatusHandler(sc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d", rec.Code)
			}
			raw := rec.Body.String()
			if strings.Contains(raw, "Jane") || strings.Contains(raw, "4875000") {
				t.Fatalf("body leaks check error: %s", raw)
			}
			var b StatusBody
			if err := json.Unmarshal([]byte(raw), &b); err != nil {
				t.Fatal(err)
			}
			if b.Status != tc.want {
				t.Errorf("status = %q, want %q", b.Status, tc.want)
			}
			if len(b.Checks) != len(tc.each) {
				t.Fatalf("checks = %+v", b.Checks)
			}
			for _, c := range b.Checks {
				if tc.each[c.Name] != c.Status {
					t.Errorf("check %s = %q, want %q", c.Name, c.Status, tc.each[c.Name])
				}
			}
		})
	}
}

func TestStatusHandlerMethod(t *testing.T) {
	tests := []struct {
		method string
		code   int
	}{
		{http.MethodGet, http.StatusOK},
		{http.MethodPost, http.StatusMethodNotAllowed},
		{http.MethodDelete, http.StatusMethodNotAllowed},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			StatusHandler(NewSmokeChecks(0)).ServeHTTP(rec, httptest.NewRequest(tc.method, "/status", nil))
			if rec.Code != tc.code {
				t.Errorf("code = %d, want %d", rec.Code, tc.code)
			}
		})
	}
}

func TestNewStatusBodyDegradedFlag(t *testing.T) {
	tests := []struct {
		name string
		st   Status
		want string
	}{
		{"flag only", Status{Degraded: true}, StatusDegraded},
		{"failed check without flag", Status{Checks: []CheckResult{{Name: "x", OK: false}}}, StatusDegraded},
		{"ok", Status{Checks: []CheckResult{{Name: "x", OK: true, Took: 1500 * time.Millisecond}}}, StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewStatusBody(tc.st).Status; got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}
