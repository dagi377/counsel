package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestARC_I_012_CheckWrites(t *testing.T) {
	owned := map[string]string{"invoice.status=approved": "sales.invoice_approval"}
	approve := Route{Method: http.MethodPost, Pattern: BasePath + "/invoices/{id}/approve"}
	read := Route{Method: http.MethodGet, Pattern: BasePath + "/invoices/{id}"}
	edit := Route{Method: http.MethodPatch, Pattern: BasePath + "/invoices/{id}"}
	tests := []struct {
		name   string
		served []Route
		writes []RouteWrite
		want   []string // substrings, one per expected mismatch
	}{
		{"read routes need no declaration", []Route{read}, nil, nil},
		{"undeclared write", []Route{edit}, nil, []string{"no write declaration: PATCH"}},
		{"undeclared delete", []Route{{Method: http.MethodDelete, Pattern: BasePath + "/x"}}, nil, []string{"no write declaration: DELETE"}},
		{"owned key on a plain route", []Route{approve},
			[]RouteWrite{{Route: approve, Sets: []string{"invoice.status=approved"}}},
			[]string{"gate-owned state invoice.status=approved"}},
		{"owned key on the decision route", []Route{DecisionRoute},
			[]RouteWrite{{Route: DecisionRoute, Sets: []string{"invoice.status=approved"}}}, nil},
		{"undeclared decision route", []Route{DecisionRoute}, nil, []string{"no write declaration: POST " + DecisionRoute.Pattern}},
		{"clean declaration", []Route{edit, read},
			[]RouteWrite{{Route: edit, Sets: []string{"invoice.memo"}}}, nil},
		{"gate-opening route declares nothing", []Route{approve},
			[]RouteWrite{{Route: approve}}, nil},
		{"declaration for an unserved route is ignored", nil,
			[]RouteWrite{{Route: approve, Sets: []string{"invoice.status=approved"}}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckWrites(tc.served, tc.writes, owned, DecisionRoute)
			if len(got) != len(tc.want) {
				t.Fatalf("CheckWrites = %q, want %d mismatches", got, len(tc.want))
			}
			for i, w := range tc.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("mismatch %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}

func TestDeclaredWritesWalksMountedRoutes(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request) {}
	m := New(nil)
	err := m.Mount(func(r chi.Router) {
		r.Get("/invoices/{id}", noop)
		r.Method(http.MethodPost, "/invoices", Declare(noop))
		r.With(func(h http.Handler) http.Handler { return h }).
			Method(http.MethodPatch, "/invoices/{id}", Declare(noop, "invoice.memo"))
		r.Delete("/invoices/{id}", noop)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DeclaredWrites(m.Handler())
	if err != nil {
		t.Fatal(err)
	}
	want := map[Route][]string{
		{Method: http.MethodPost, Pattern: BasePath + "/invoices"}:       nil,
		{Method: http.MethodPatch, Pattern: BasePath + "/invoices/{id}"}: {"invoice.memo"},
	}
	if len(got) != len(want) {
		t.Fatalf("DeclaredWrites = %+v, want %d", got, len(want))
	}
	for _, w := range got {
		sets, ok := want[w.Route]
		if !ok || !slices.Equal(sets, w.Sets) {
			t.Errorf("unexpected declaration %+v", w)
		}
	}
	mis := CheckWrites(m.Routes(), got, nil, DecisionRoute)
	if len(mis) != 1 || !strings.Contains(mis[0], "DELETE") {
		t.Errorf("CheckWrites = %q, want only the undeclared DELETE", mis)
	}
	if _, err := DeclaredWrites(http.NotFoundHandler()); err == nil {
		t.Error("DeclaredWrites on a non-chi handler: want error")
	}
}
