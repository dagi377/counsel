package practiceapi

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DefaultPageSize is the page size used when a request names no limit.
const DefaultPageSize = 200

// MaxPageSize caps the limit query parameter.
const MaxPageSize = 200

var _ Server = (*HTTPServer)(nil)

// HTTPServer serves loaded records as paged v4-shaped list responses and records every
// request. Start binds it to the loopback interface only.
type HTTPServer struct {
	mu        sync.Mutex
	pageSize  int
	resources map[string][]json.RawMessage
	requests  []Request
}

// NewServer returns an empty server with the given default page size (1..MaxPageSize).
func NewServer(pageSize int) *HTTPServer {
	if pageSize < 1 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}
	return &HTTPServer{pageSize: pageSize, resources: map[string][]json.RawMessage{}}
}

// Load replaces the records of resource.
func (s *HTTPServer) Load(resource string, records []json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource] = append([]json.RawMessage{}, records...)
}

// LoadFS loads every <resource>.json file in dir of fsys. Each file holds a JSON array of
// records, served in file order.
func (s *HTTPServer) LoadFS(fsys fs.FS, dir string) error {
	matches, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return fmt.Errorf("load testdata %s: %w", dir, err)
	}
	sort.Strings(matches)
	for _, m := range matches {
		b, err := fs.ReadFile(fsys, m)
		if err != nil {
			return fmt.Errorf("load testdata %s: %w", m, err)
		}
		var records []json.RawMessage
		if err := json.Unmarshal(b, &records); err != nil {
			return fmt.Errorf("load testdata %s: %w", m, err)
		}
		s.Load(strings.TrimSuffix(path.Base(m), ".json"), records)
	}
	return nil
}

// Requests returns every request received, in order, including rejected ones.
func (s *HTTPServer) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// NonGET returns the recorded requests whose method is not GET.
func (s *HTTPServer) NonGET() []Request {
	var out []Request
	for _, r := range s.Requests() {
		if r.Method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// Start serves the handler on 127.0.0.1 at a free port. Close the returned server when done.
func (s *HTTPServer) Start() (*httptest.Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen loopback: %w", err)
	}
	ts := &httptest.Server{Listener: ln, Config: &http.Server{Handler: s.Handler()}}
	ts.Start()
	return ts, nil
}

// Handler serves GET {APIPrefix}<resource>.json?page_token=N&limit=M; any other method
// answers 405.
func (s *HTTPServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery})
		s.mu.Unlock()
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, okPrefix := strings.CutPrefix(r.URL.Path, APIPrefix)
		name, okSuffix := strings.CutSuffix(name, ".json")
		s.mu.Lock()
		all, loaded := s.resources[name]
		size := s.pageSize
		s.mu.Unlock()
		if !okPrefix || !okSuffix || !loaded {
			http.Error(w, ErrUnknownResource.Error(), http.StatusNotFound)
			return
		}
		q := r.URL.Query()
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > MaxPageSize {
				http.Error(w, "bad limit", http.StatusBadRequest)
				return
			}
			size = n
		}
		off := 0
		if v := q.Get("page_token"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > len(all) {
				http.Error(w, "bad page_token", http.StatusBadRequest)
				return
			}
			off = n
		}
		end := min(off+size, len(all))
		resp := ListResponse{Data: append([]json.RawMessage{}, all[off:end]...), Meta: Meta{Count: len(all)}}
		if end < len(all) {
			next := q
			next.Set("page_token", strconv.Itoa(end))
			resp.Meta.Paging.Next = fmt.Sprintf("http://%s%s%s.json?%s", r.Host, APIPrefix, name, next.Encode())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
