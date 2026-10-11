package importer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

var errKilled = errors.New("killed")

// pagedSource serves pages of one resource. A non-zero killAfter fails the request for
// the page after it, like a process killed between pages.
type pagedSource struct {
	resource  string
	sizes     []int
	killAfter int
	requests  []int // page numbers requested, 1-based
}

func (s *pagedSource) Resources() []string { return []string{s.resource} }

func (s *pagedSource) Extract(_ context.Context, res string, cur Cursor) (Page, error) {
	if res != s.resource {
		return Page{}, fmt.Errorf("resource %q: %w", res, ErrUnmapped)
	}
	n := 1
	if cur != "" {
		v, err := strconv.Atoi(string(cur))
		if err != nil {
			return Page{}, err
		}
		n = v
	}
	s.requests = append(s.requests, n)
	if s.killAfter > 0 && n == s.killAfter+1 {
		return Page{}, errKilled
	}
	start := 0
	for _, sz := range s.sizes[:n-1] {
		start += sz
	}
	p := Page{Next: Cursor(strconv.Itoa(n + 1)), Done: n == len(s.sizes)}
	for i := 0; i < s.sizes[n-1]; i++ {
		p.Rows = append(p.Rows, Row{Resource: res, ExternalID: strconv.Itoa(start + i + 1)})
	}
	return p, nil
}

func (s *pagedSource) Transform(context.Context, Row, IDMap) ([]Command, error) { return nil, nil }

// 94 pages of 200 and one of 130: 18,930 rows.
func docSizes() []int {
	sizes := make([]int, 95)
	for i := range sizes {
		sizes[i] = 200
	}
	sizes[94] = 130
	return sizes
}

func TestMIG_004_ResumeAfterKill(t *testing.T) {
	// rule: MIG-004
	tests := []struct {
		name      string
		killAfter int
		wantFirst int // first page requested on restart
	}{
		{"killed after page 37 of 95", 37, 38},
		{"killed after page 1", 1, 2},
		{"killed after page 94", 94, 95},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemCheckpoints()
			src := &pagedSource{resource: "documents", sizes: docSizes(), killAfter: tt.killAfter}
			if err := ExtractResource(ctx, store, "src", src, "documents"); !errors.Is(err, errKilled) {
				t.Fatalf("first run: got %v, want killed", err)
			}
			src.killAfter, src.requests = 0, nil
			if err := ExtractResource(ctx, store, "src", src, "documents"); err != nil {
				t.Fatalf("restart: %v", err)
			}
			if src.requests[0] != tt.wantFirst {
				t.Errorf("restart fetched page %d first, want %d", src.requests[0], tt.wantFirst)
			}
			distinct, written := store.Staged("src", "documents")
			if distinct != 18930 || written != 18930 {
				t.Errorf("staged distinct=%d written=%d, want 18930 both", distinct, written)
			}
			cp, found, err := store.Load(ctx, "src", "documents")
			if err != nil || !found || !cp.Done {
				t.Errorf("checkpoint = %+v found=%v err=%v, want done", cp, found, err)
			}
		})
	}
}

func TestMIG_004_RestageSamePageIsNoop(t *testing.T) {
	// rule: MIG-004
	ctx := context.Background()
	page := Page{Rows: []Row{{Resource: "r", ExternalID: "1"}, {Resource: "r", ExternalID: "2"}}, Next: "2"}
	sha, err := PageSHA(page)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		commits     int
		wantWritten int
	}{
		{"once", 1, 2},
		{"three times", 3, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemCheckpoints()
			for i := 0; i < tt.commits; i++ {
				cp := Checkpoint{Source: "s", Resource: "r", Cursor: page.Next, PageSHA: sha}
				if err := store.Commit(ctx, cp, page); err != nil {
					t.Fatal(err)
				}
			}
			if _, written := store.Staged("s", "r"); written != tt.wantWritten {
				t.Errorf("written = %d, want %d", written, tt.wantWritten)
			}
		})
	}
}

func TestMIG_004_DoneResourceIsNotRefetched(t *testing.T) {
	// rule: MIG-004
	ctx := context.Background()
	store := NewMemCheckpoints()
	src := &pagedSource{resource: "r", sizes: []int{2, 1}}
	for run := 1; run <= 2; run++ {
		src.requests = nil
		if err := ExtractAll(ctx, store, "s", src); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		want := map[int]int{1: 2, 2: 0}[run]
		if len(src.requests) != want {
			t.Errorf("run %d: %d requests, want %d", run, len(src.requests), want)
		}
	}
}

func TestSourcesRegister(t *testing.T) {
	src := &pagedSource{resource: "r", sizes: []int{1}}
	tests := []struct {
		name    string
		ids     []string
		lookup  string
		wantReg error
		wantGet error
	}{
		{"registered", []string{"a"}, "a", nil, nil},
		{"unknown", []string{"a"}, "b", nil, ErrUnknownSource},
		{"duplicate", []string{"a", "a"}, "a", ErrDuplicateSource, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewSources()
			var regErr error
			for _, id := range tt.ids {
				if err := r.Register(id, src); err != nil {
					regErr = err
				}
			}
			if !errors.Is(regErr, tt.wantReg) {
				t.Errorf("register err = %v, want %v", regErr, tt.wantReg)
			}
			if _, err := r.Get(tt.lookup); !errors.Is(err, tt.wantGet) {
				t.Errorf("get err = %v, want %v", err, tt.wantGet)
			}
		})
	}
	if err := NewSources().Register("", src); err == nil {
		t.Error("empty id registered")
	}
}
