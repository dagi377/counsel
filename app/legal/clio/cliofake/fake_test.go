package cliofake

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/clio"
)

func recs(n int) []json.RawMessage {
	out := make([]json.RawMessage, n)
	for i := range out {
		out[i] = json.RawMessage(`{"id":` + string(rune('0'+i)) + `}`)
	}
	return out
}

func TestFakeList(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		pageSize int
		pages    []int
	}{
		{"empty", 0, 2, []int{0}},
		{"exact pages", 4, 2, []int{2, 2}},
		{"short last page", 5, 2, []int{2, 2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(tt.pageSize)
			f.Seed(clio.ResourceMatters, recs(tt.n)...)
			var got []int
			token := ""
			for {
				p, err := f.List(context.Background(), clio.ResourceMatters, token)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, len(p.Records))
				if p.Next == "" {
					break
				}
				token = p.Next
			}
			if len(got) != len(tt.pages) {
				t.Fatalf("pages = %v, want %v", got, tt.pages)
			}
			for i := range got {
				if got[i] != tt.pages[i] {
					t.Fatalf("pages = %v, want %v", got, tt.pages)
				}
			}
		})
	}
	if _, err := New(2).List(context.Background(), clio.ResourceMatters, "x"); !errors.Is(err, clio.ErrBadPageToken) {
		t.Fatalf("bad token: %v", err)
	}
}

func TestFakeDryRun(t *testing.T) {
	f := New(2)
	f.Seed(clio.ResourceContacts, recs(3)...)
	f.Seed(clio.ResourceMatters, recs(1)...)
	rep, err := f.DryRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		res  clio.Resource
		want clio.Count
	}{
		{clio.ResourceContacts, clio.Count{Found: 3, Create: 3}},
		{clio.ResourceMatters, clio.Count{Found: 1, Create: 1}},
	}
	for _, tt := range tests {
		if got := rep.Resources[tt.res]; got != tt.want {
			t.Errorf("%s = %+v, want %+v", tt.res, got, tt.want)
		}
	}
	if f.Waits() != 3 {
		t.Fatalf("Waits = %d, want one per page (3)", f.Waits())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.DryRun(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled dry run: %v", err)
	}
}
