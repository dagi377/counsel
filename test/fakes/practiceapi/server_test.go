package practiceapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
)

func records(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`{"id":%d}`, i+1)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func testdata() fstest.MapFS {
	return fstest.MapFS{
		"testdata/contacts.json": {Data: []byte(records(5))},
		"testdata/notes.json":    {Data: []byte(records(0))},
	}
}

func start(t *testing.T, pageSize int) (*HTTPServer, string) {
	t.Helper()
	s := NewServer(pageSize)
	if err := s.LoadFS(testdata(), "testdata"); err != nil {
		t.Fatal(err)
	}
	ts, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	return s, ts.URL
}

func get(t *testing.T, u string) (int, ListResponse) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body ListResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp.StatusCode, body
}

func TestPagingFollowsNext(t *testing.T) {
	tests := []struct {
		name      string
		resource  string
		pageSize  int
		query     string
		wantPages []int
	}{
		{"two per page", "contacts", 2, "", []int{2, 2, 1}},
		{"limit overrides default", "contacts", 2, "?limit=4", []int{4, 1}},
		{"one page", "contacts", 10, "", []int{5}},
		{"empty resource", "notes", 2, "", []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, base := start(t, tt.pageSize)
			next := base + APIPrefix + tt.resource + ".json" + tt.query
			var got []int
			total := 0
			for next != "" {
				code, body := get(t, next)
				if code != http.StatusOK {
					t.Fatalf("GET %s: %d", next, code)
				}
				got = append(got, len(body.Data))
				total = body.Meta.Count
				next = body.Meta.Paging.Next
				if next != "" {
					if u, err := url.Parse(next); err != nil || u.Hostname() != "127.0.0.1" {
						t.Fatalf("next %q is not a loopback url", next)
					}
				}
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.wantPages) {
				t.Errorf("page sizes = %v, want %v", got, tt.wantPages)
			}
			sum := 0
			for _, n := range got {
				sum += n
			}
			if sum != total {
				t.Errorf("served %d rows, meta.records = %d", sum, total)
			}
			if len(s.Requests()) != len(tt.wantPages) || len(s.NonGET()) != 0 {
				t.Errorf("requests = %v", s.Requests())
			}
		})
	}
}

func TestRecordsEveryMethod(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		wantCode int
	}{
		{http.MethodGet, APIPrefix + "contacts.json", http.StatusOK},
		{http.MethodPost, APIPrefix + "contacts.json", http.StatusMethodNotAllowed},
		{http.MethodPatch, APIPrefix + "contacts/1.json", http.StatusMethodNotAllowed},
		{http.MethodDelete, APIPrefix + "contacts/1.json", http.StatusMethodNotAllowed},
		{http.MethodGet, APIPrefix + "unknown.json", http.StatusNotFound},
		{http.MethodGet, APIPrefix + "contacts.json?limit=0", http.StatusBadRequest},
		{http.MethodGet, APIPrefix + "contacts.json?page_token=x", http.StatusBadRequest},
	}
	s, base := start(t, 2)
	for _, tt := range tests {
		req, err := http.NewRequest(tt.method, base+tt.path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tt.wantCode {
			t.Errorf("%s %s = %d, want %d", tt.method, tt.path, resp.StatusCode, tt.wantCode)
		}
	}
	reqs := s.Requests()
	if len(reqs) != len(tests) {
		t.Fatalf("recorded %d requests, want %d", len(reqs), len(tests))
	}
	for i, tt := range tests {
		if reqs[i].Method != tt.method {
			t.Errorf("request %d method = %s, want %s", i, reqs[i].Method, tt.method)
		}
	}
	if got := len(s.NonGET()); got != 3 {
		t.Errorf("non-GET = %d, want 3", got)
	}
}

func TestLoadFSRejectsBadJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{"array", "[]", false},
		{"object", "{}", true},
		{"garbage", "nope", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{"d/r.json": {Data: []byte(tt.data)}}
			if err := NewServer(1).LoadFS(fsys, "d"); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
