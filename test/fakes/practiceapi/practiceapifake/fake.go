// Package practiceapifake is an in-memory practiceapi.Server; it never opens a socket.
package practiceapifake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/dagi377/counsel/test/fakes/practiceapi"
)

var _ practiceapi.Server = (*Fake)(nil)

// Fake serves loaded records in pages of PageSize using a page_token offset.
type Fake struct {
	mu        sync.Mutex
	PageSize  int
	resources map[string][]json.RawMessage
	requests  []practiceapi.Request
}

// New returns a Fake with the given page size (minimum 1).
func New(pageSize int) *Fake {
	if pageSize < 1 {
		pageSize = 1
	}
	return &Fake{PageSize: pageSize, resources: map[string][]json.RawMessage{}}
}

// Load replaces the records of resource.
func (f *Fake) Load(resource string, records []json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resources[resource] = append([]json.RawMessage(nil), records...)
}

// Requests returns the recorded requests.
func (f *Fake) Requests() []practiceapi.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]practiceapi.Request(nil), f.requests...)
}

// Handler serves GET {APIPrefix}<resource>.json?page_token=N.
func (f *Fake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, practiceapi.Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery})
		f.mu.Unlock()
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, ok := strings.CutPrefix(r.URL.Path, practiceapi.APIPrefix)
		name, okSuffix := strings.CutSuffix(name, ".json")
		f.mu.Lock()
		all, loaded := f.resources[name]
		f.mu.Unlock()
		if !ok || !okSuffix || !loaded {
			http.Error(w, practiceapi.ErrUnknownResource.Error(), http.StatusNotFound)
			return
		}
		off := 0
		if tok := r.URL.Query().Get("page_token"); tok != "" {
			n, err := strconv.Atoi(tok)
			if err != nil || n < 0 || n > len(all) {
				http.Error(w, "bad page_token", http.StatusBadRequest)
				return
			}
			off = n
		}
		end := min(off+f.PageSize, len(all))
		resp := practiceapi.ListResponse{Data: all[off:end], Meta: practiceapi.Meta{Count: len(all)}}
		if resp.Data == nil {
			resp.Data = []json.RawMessage{}
		}
		if end < len(all) {
			resp.Meta.Paging.Next = fmt.Sprintf("%s%s.json?page_token=%d", practiceapi.APIPrefix, name, end)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
