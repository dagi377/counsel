package practiceapifake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dagi377/counsel/test/fakes/practiceapi"
)

func TestFakeHandler(t *testing.T) {
	f := New(2)
	f.Load("matters", []json.RawMessage{json.RawMessage(`{"id":1}`), json.RawMessage(`{"id":2}`), json.RawMessage(`{"id":3}`)})
	f.Load("contacts", nil)
	tests := []struct {
		name   string
		method string
		target string
		status int
		n      int
		next   string
	}{
		{"first page", http.MethodGet, "/api/v4/matters.json", 200, 2, "/api/v4/matters.json?page_token=2"},
		{"last page", http.MethodGet, "/api/v4/matters.json?page_token=2", 200, 1, ""},
		{"empty resource", http.MethodGet, "/api/v4/contacts.json", 200, 0, ""},
		{"unknown resource", http.MethodGet, "/api/v4/bills.json", 404, 0, ""},
		{"bad token", http.MethodGet, "/api/v4/matters.json?page_token=x", 400, 0, ""},
		{"write refused", http.MethodPost, "/api/v4/matters.json", 405, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			f.Handler().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, nil))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.status != 200 {
				return
			}
			var resp practiceapi.ListResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if len(resp.Data) != tt.n || resp.Meta.Paging.Next != tt.next {
				t.Fatalf("resp = %d records next %q", len(resp.Data), resp.Meta.Paging.Next)
			}
		})
	}
	reqs := f.Requests()
	if len(reqs) != len(tests) || reqs[len(reqs)-1].Method != http.MethodPost {
		t.Fatalf("requests = %+v", reqs)
	}
}
