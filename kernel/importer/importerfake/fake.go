// Package importerfake is an in-memory implementation of the kernel/importer contract for tests.
package importerfake

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/dagi377/counsel/kernel/importer"
)

var (
	_ importer.Importer = (*Fake)(nil)
	_ importer.IDMap    = (*IDMap)(nil)
)

type mapKey struct{ source, resource, ext string }

// IDMap is an in-memory id map that hands out sequential internal ids.
type IDMap struct {
	mu  sync.Mutex
	seq int
	ids map[mapKey]string
}

// NewIDMap returns an empty IDMap.
func NewIDMap() *IDMap { return &IDMap{ids: map[mapKey]string{}} }

// Lookup returns the internal id for an external one.
func (m *IDMap) Lookup(_ context.Context, source, resource, ext string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.ids[mapKey{source, resource, ext}]
	return id, ok, nil
}

// Assign returns the existing id or creates one.
func (m *IDMap) Assign(_ context.Context, source, resource, ext string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mapKey{source, resource, ext}
	if id, ok := m.ids[k]; ok {
		return id, nil
	}
	m.seq++
	id := "id-" + strconv.Itoa(m.seq)
	m.ids[k] = id
	return id, nil
}

// Len returns the number of id map entries.
func (m *IDMap) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ids)
}

// Fake runs sources in memory. Applied commands are kept in Applied. A Fake is safe for
// concurrent use, but runs one operation at a time.
type Fake struct {
	mu          sync.Mutex
	seq         int
	sources     map[string]importer.Source
	dryRuns     map[string]importer.DryRun
	checkpoints map[[2]string]importer.Cursor
	IDs         *IDMap
	Applied     []importer.Command
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{
		sources: map[string]importer.Source{}, dryRuns: map[string]importer.DryRun{},
		checkpoints: map[[2]string]importer.Cursor{}, IDs: NewIDMap(),
	}
}

// Register adds a source.
func (f *Fake) Register(id string, s importer.Source) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.sources[id]; ok {
		return fmt.Errorf("register %q: %w", id, importer.ErrDuplicateSource)
	}
	f.sources[id] = s
	return nil
}

func (f *Fake) source(id string) (importer.Source, error) {
	s, ok := f.sources[id]
	if !ok {
		return nil, fmt.Errorf("source %q: %w", id, importer.ErrUnknownSource)
	}
	return s, nil
}

// DryRun extracts every resource from the start and transforms into a scratch id map.
func (f *Fake) DryRun(ctx context.Context, source, snapshot string) (importer.DryRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.source(source)
	if err != nil {
		return importer.DryRun{}, err
	}
	f.seq++
	d := importer.DryRun{
		ID: "dry-" + strconv.Itoa(f.seq), Source: source, Snapshot: snapshot,
		Counts: map[string]int{}, Unmapped: map[string]int{},
	}
	scratch := NewIDMap()
	for _, res := range s.Resources() {
		cur := importer.Cursor("")
		for {
			p, err := s.Extract(ctx, res, cur)
			if err != nil {
				return importer.DryRun{}, fmt.Errorf("dry run %s: extract %s: %w", source, res, err)
			}
			for _, row := range p.Rows {
				d.Counts[res]++
				if _, err := s.Transform(ctx, row, scratch); err != nil {
					if errors.Is(err, importer.ErrUnmapped) {
						d.Unmapped[res]++
					}
					d.Errors = append(d.Errors, importer.Issue{Resource: res, ExternalID: row.ExternalID, Message: err.Error()})
				}
			}
			if p.Done {
				break
			}
			cur = p.Next
		}
	}
	f.dryRuns[d.ID] = d
	return d, nil
}

// Approve approves a zero-error dry run.
func (f *Fake) Approve(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.dryRuns[id]
	if !ok {
		return fmt.Errorf("approve %q: %w", id, importer.ErrNoApprovedDryRun)
	}
	if len(d.Errors) > 0 {
		return fmt.Errorf("approve %q: %w", id, importer.ErrDryRunHasErrors)
	}
	d.Approved = true
	f.dryRuns[id] = d
	return nil
}

// Run imports the snapshot from the saved checkpoints, saving a checkpoint after each page.
func (f *Fake) Run(ctx context.Context, source, snapshot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.source(source)
	if err != nil {
		return err
	}
	approved := false
	for _, d := range f.dryRuns {
		if d.Source == source && d.Snapshot == snapshot && d.Approved {
			approved = true
		}
	}
	if !approved {
		return fmt.Errorf("run %s: %w", source, importer.ErrNoApprovedDryRun)
	}
	for _, res := range s.Resources() {
		k := [2]string{source, res}
		for {
			cur := f.checkpoints[k]
			if cur == doneCursor {
				break
			}
			p, err := s.Extract(ctx, res, cur)
			if err != nil {
				return fmt.Errorf("run %s: extract %s: %w", source, res, err)
			}
			for _, row := range p.Rows {
				cmds, err := s.Transform(ctx, row, f.IDs)
				if err != nil {
					return fmt.Errorf("run %s: transform %s: %w", source, res, err)
				}
				f.Applied = append(f.Applied, cmds...)
			}
			if p.Done {
				f.checkpoints[k] = doneCursor
				break
			}
			f.checkpoints[k] = p.Next
		}
	}
	return nil
}

// doneCursor marks a fully extracted resource.
const doneCursor importer.Cursor = "\x00done"

// Checkpoint returns the saved cursor for a resource.
func (f *Fake) Checkpoint(_ context.Context, source, resource string) (importer.Cursor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.source(source); err != nil {
		return "", err
	}
	return f.checkpoints[[2]string{source, resource}], nil
}
