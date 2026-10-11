// Package cliofake is an in-memory clio.Source, clio.Limiter and clio.Importer.
package cliofake

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/dagi377/counsel/app/legal/clio"
)

var (
	_ clio.Source   = (*Fake)(nil)
	_ clio.Limiter  = (*Fake)(nil)
	_ clio.Importer = (*Fake)(nil)
)

// Fake serves seeded records in pages of PageSize and counts Wait calls.
type Fake struct {
	mu       sync.Mutex
	PageSize int
	records  map[clio.Resource][]json.RawMessage
	waits    int
	lists    int
}

// New returns a Fake with the given page size (minimum 1).
func New(pageSize int) *Fake {
	if pageSize < 1 {
		pageSize = 1
	}
	return &Fake{PageSize: pageSize, records: map[clio.Resource][]json.RawMessage{}}
}

// Seed appends records to res.
func (f *Fake) Seed(res clio.Resource, records ...json.RawMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[res] = append(f.records[res], records...)
}

// List pages through res; the page token is the decimal offset.
func (f *Fake) List(_ context.Context, res clio.Resource, pageToken string) (clio.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	off := 0
	if pageToken != "" {
		n, err := strconv.Atoi(pageToken)
		if err != nil || n < 0 || n > len(f.records[res]) {
			return clio.Page{}, fmt.Errorf("list %s: %w", res, clio.ErrBadPageToken)
		}
		off = n
	}
	all := f.records[res]
	end := min(off+f.PageSize, len(all))
	p := clio.Page{Records: append([]json.RawMessage(nil), all[off:end]...)}
	if end < len(all) {
		p.Next = strconv.Itoa(end)
	}
	return p, nil
}

// Wait never blocks; it only counts calls.
func (f *Fake) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waits++
	return nil
}

// Waits returns how many times Wait was called.
func (f *Fake) Waits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waits
}

// DryRun reports every seeded record as Found and Create.
func (f *Fake) DryRun(ctx context.Context) (clio.DryRunReport, error) {
	rep := clio.DryRunReport{Resources: map[clio.Resource]clio.Count{}}
	f.mu.Lock()
	var resources []clio.Resource
	for r := range f.records {
		resources = append(resources, r)
	}
	f.mu.Unlock()
	for _, r := range resources {
		token := ""
		var c clio.Count
		for {
			if err := f.Wait(ctx); err != nil {
				return clio.DryRunReport{}, fmt.Errorf("dry run %s: %w", r, err)
			}
			p, err := f.List(ctx, r, token)
			if err != nil {
				return clio.DryRunReport{}, fmt.Errorf("dry run %s: %w", r, err)
			}
			c.Found += len(p.Records)
			c.Create += len(p.Records)
			if p.Next == "" {
				break
			}
			token = p.Next
		}
		rep.Resources[r] = c
	}
	return rep, nil
}
