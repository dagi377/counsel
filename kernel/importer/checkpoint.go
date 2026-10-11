package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// Checkpoint is the committed extraction position of one resource of one source.
type Checkpoint struct {
	Source   string
	Resource string
	Cursor   Cursor // resumes after the last committed page
	PageSHA  string // sha256 of the last committed page
	Done     bool   // the last page was committed
}

// CheckpointStore commits a staged page and its cursor together and returns the last commit.
type CheckpointStore interface {
	// Load returns the last committed checkpoint; found is false before the first page.
	Load(ctx context.Context, source, resource string) (cp Checkpoint, found bool, err error)
	// Commit stages the page rows and moves the cursor in one transaction. Committing a
	// page whose sha256 was already staged for the resource changes nothing.
	Commit(ctx context.Context, cp Checkpoint, page Page) error
}

// PageSHA returns the hex sha256 of a page's rows, the key that makes re-staging a no-op.
func PageSHA(p Page) (string, error) {
	b, err := json.Marshal(p.Rows)
	if err != nil {
		return "", fmt.Errorf("hash page: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ExtractResource pulls one resource of a source page by page, committing each page with
// its cursor before asking for the next. It starts from the last committed cursor, so a
// run killed between pages resumes at the first page that was not committed.
//
// rule: MIG-004
func ExtractResource(ctx context.Context, store CheckpointStore, sourceID string, s Source, resource string) error {
	cp, _, err := store.Load(ctx, sourceID, resource)
	if err != nil {
		return fmt.Errorf("extract %s/%s: load checkpoint: %w", sourceID, resource, err)
	}
	for !cp.Done {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extract %s/%s: %w", sourceID, resource, err)
		}
		page, err := s.Extract(ctx, resource, cp.Cursor)
		if err != nil {
			return fmt.Errorf("extract %s/%s: %w", sourceID, resource, err)
		}
		sha, err := PageSHA(page)
		if err != nil {
			return fmt.Errorf("extract %s/%s: %w", sourceID, resource, err)
		}
		next := Checkpoint{Source: sourceID, Resource: resource, Cursor: page.Next, PageSHA: sha, Done: page.Done}
		// rule: MIG-004
		if err := store.Commit(ctx, next, page); err != nil {
			return fmt.Errorf("extract %s/%s: commit page: %w", sourceID, resource, err)
		}
		cp = next
	}
	return nil
}

// ExtractAll extracts every resource of the source, in the order the source lists them.
func ExtractAll(ctx context.Context, store CheckpointStore, sourceID string, s Source) error {
	for _, res := range s.Resources() {
		if err := ExtractResource(ctx, store, sourceID, s, res); err != nil {
			return err
		}
	}
	return nil
}

type cpKey struct{ source, resource string }

type rowKey struct{ source, resource, externalID string }

// MemCheckpoints is an in-memory CheckpointStore that also keeps the staged rows.
// It is safe for concurrent use.
type MemCheckpoints struct {
	mu     sync.Mutex
	cps    map[cpKey]Checkpoint
	shas   map[cpKey]map[string]bool
	staged map[rowKey]Row
	writes map[cpKey]int // rows written, duplicates included
}

// NewMemCheckpoints returns an empty store.
func NewMemCheckpoints() *MemCheckpoints {
	return &MemCheckpoints{
		cps: map[cpKey]Checkpoint{}, shas: map[cpKey]map[string]bool{}, staged: map[rowKey]Row{}, writes: map[cpKey]int{},
	}
}

// Load returns the last committed checkpoint.
func (m *MemCheckpoints) Load(_ context.Context, source, resource string) (Checkpoint, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp, ok := m.cps[cpKey{source, resource}]
	if !ok {
		return Checkpoint{Source: source, Resource: resource}, false, nil
	}
	return cp, true, nil
}

// Commit stages the rows and stores the checkpoint under one lock.
func (m *MemCheckpoints) Commit(_ context.Context, cp Checkpoint, page Page) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := cpKey{cp.Source, cp.Resource}
	seen := m.shas[k]
	if seen == nil {
		seen = map[string]bool{}
		m.shas[k] = seen
	}
	// rule: MIG-004
	if seen[cp.PageSHA] {
		return nil
	}
	seen[cp.PageSHA] = true
	for _, r := range page.Rows {
		m.staged[rowKey{cp.Source, r.Resource, r.ExternalID}] = r
	}
	m.writes[k] += len(page.Rows)
	m.cps[k] = cp
	return nil
}

// Staged returns the distinct staged rows and the rows written (duplicates included) for
// a resource of a source.
func (m *MemCheckpoints) Staged(source, resource string) (distinct, written int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.staged {
		if k.source == source && k.resource == resource {
			n++
		}
	}
	return n, m.writes[cpKey{source, resource}]
}
