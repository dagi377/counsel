// Package commsfake is an in-memory comms.Service for tests of other modules.
package commsfake

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/comms"
)

var _ comms.Service = (*Fake)(nil)

var placeholder = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// Fake keeps shared items, filings and templates in memory. Now defaults to time.Now.
type Fake struct {
	mu        sync.Mutex
	Now       func() time.Time
	items     []comms.SharedItem
	filings   []comms.Filing
	templates map[string]comms.Template
}

// New returns an empty Fake.
func New() *Fake { return &Fake{Now: time.Now, templates: map[string]comms.Template{}} }

// Share records item.
func (f *Fake) Share(_ context.Context, item comms.SharedItem) error {
	if item.ID == "" || item.MatterID == "" || (item.Kind != comms.ItemDocument && item.Kind != comms.ItemBill) {
		return fmt.Errorf("share: %w", comms.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, item)
	return nil
}

// Matters lists the guest's matters with shared items.
func (f *Fake) Matters(_ context.Context, g comms.Guest) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, it := range f.items {
		if slices.Contains(g.MatterIDs, it.MatterID) && !slices.Contains(out, it.MatterID) {
			out = append(out, it.MatterID)
		}
	}
	slices.Sort(out)
	return out, nil
}

// Items lists shared items of matterID if the guest is on it.
func (f *Fake) Items(_ context.Context, g comms.Guest, matterID string) ([]comms.SharedItem, error) {
	if !slices.Contains(g.MatterIDs, matterID) {
		return nil, comms.ErrNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []comms.SharedItem
	for _, it := range f.items {
		if it.MatterID == matterID {
			out = append(out, it)
		}
	}
	return out, nil
}

// RecordFiling appends a filing.
func (f *Fake) RecordFiling(_ context.Context, fl comms.Filing) (comms.Filing, error) {
	if fl.MatterID == "" || fl.Court == "" || fl.Status == "" {
		return comms.Filing{}, fmt.Errorf("record filing: %w", comms.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fl.ID = fmt.Sprintf("filing-%d", len(f.filings)+1)
	fl.RecordedAt = f.Now()
	f.filings = append(f.filings, fl)
	return fl, nil
}

// FilingsFor lists a matter's filings.
func (f *Fake) FilingsFor(_ context.Context, matterID string) ([]comms.Filing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []comms.Filing
	for _, fl := range f.filings {
		if fl.MatterID == matterID {
			out = append(out, fl)
		}
	}
	return out, nil
}

// PutTemplate stores t.
func (f *Fake) PutTemplate(_ context.Context, t comms.Template) error {
	if t.ID == "" {
		return fmt.Errorf("put template: %w", comms.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.templates[t.ID] = t
	return nil
}

// Render fills {{name}} placeholders from vars.
func (f *Fake) Render(_ context.Context, id string, vars map[string]string) (string, string, error) {
	f.mu.Lock()
	t, ok := f.templates[id]
	f.mu.Unlock()
	if !ok {
		return "", "", fmt.Errorf("render %s: %w", id, comms.ErrNotFound)
	}
	subject, err := fill(t.Subject, vars)
	if err != nil {
		return "", "", fmt.Errorf("render %s subject: %w", id, err)
	}
	body, err := fill(t.Body, vars)
	if err != nil {
		return "", "", fmt.Errorf("render %s body: %w", id, err)
	}
	return subject, body, nil
}

func fill(s string, vars map[string]string) (string, error) {
	var missing string
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("%s: %w", missing, comms.ErrMissingVar)
	}
	return out, nil
}
