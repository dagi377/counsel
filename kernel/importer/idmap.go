package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// ErrUnresolved is returned when a staged row references an external id that has no id map
// entry. It is a dry-run error, never a silent null.
var ErrUnresolved = errors.New("importer: unresolved reference")

var _ IDMap = (*MemIDMap)(nil)

type idKey struct{ source, resource, externalID string }

// MemIDMap is an in-memory id map. Internal ids are derived from the source key, so the same
// (source, resource, external id) always maps to the same internal id.
type MemIDMap struct {
	mu  sync.Mutex
	ids map[idKey]string
}

// NewMemIDMap returns an empty id map.
func NewMemIDMap() *MemIDMap { return &MemIDMap{ids: map[idKey]string{}} }

// Lookup returns the internal id for an external one.
func (m *MemIDMap) Lookup(_ context.Context, source, resource, externalID string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.ids[idKey{source, resource, externalID}]
	return id, ok, nil
}

// Assign returns the existing internal id, or creates one. The map is unique on the source
// key, so a rerun returns the same id and adds no entry.
//
// rule: MIG-007
func (m *MemIDMap) Assign(_ context.Context, source, resource, externalID string) (string, error) {
	if source == "" || resource == "" || externalID == "" {
		return "", fmt.Errorf("assign id: empty source key %q/%q/%q", source, resource, externalID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := idKey{source, resource, externalID}
	if id, ok := m.ids[k]; ok {
		return id, nil
	}
	id := InternalID(source, resource, externalID)
	m.ids[k] = id
	return id, nil
}

// Len returns the number of id map entries.
func (m *MemIDMap) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ids)
}

// InternalID derives the internal id for a source key: a hex sha256 prefix of the key. It is
// a pure function, so a transform rerun on a fresh map yields the same ids.
func InternalID(source, resource, externalID string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + resource + "\x00" + externalID))
	return hex.EncodeToString(sum[:16])
}

// Resolve returns the internal id of a referenced external row. A reference with no id map
// entry returns ErrUnresolved, which the caller reports as a dry-run error.
//
// rule: MIG-007
func Resolve(ctx context.Context, m IDMap, source, resource, externalID string) (string, error) {
	id, ok, err := m.Lookup(ctx, source, resource, externalID)
	if err != nil {
		return "", fmt.Errorf("resolve %s:%s: %w", resource, externalID, err)
	}
	if !ok {
		return "", fmt.Errorf("%w %s:%s", ErrUnresolved, resource, externalID)
	}
	return id, nil
}

// TransformRows assigns every staged row exactly one id map entry, then transforms it.
// Rows are processed in (resource, external id) order so the output does not depend on
// staging order.
//
// rule: MIG-007
// rule: MIG-008
func TransformRows(ctx context.Context, sourceID string, s Source, rows []Row, m IDMap) ([]Command, error) {
	sorted := append([]Row(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Resource != sorted[j].Resource {
			return sorted[i].Resource < sorted[j].Resource
		}
		return sorted[i].ExternalID < sorted[j].ExternalID
	})
	for _, r := range sorted {
		if _, err := m.Assign(ctx, sourceID, r.Resource, r.ExternalID); err != nil {
			return nil, fmt.Errorf("transform %s: %w", sourceID, err)
		}
	}
	var out []Command
	for _, r := range sorted {
		cmds, err := s.Transform(ctx, r, m)
		if err != nil {
			return nil, fmt.Errorf("transform %s %s:%s: %w", sourceID, r.Resource, r.ExternalID, err)
		}
		out = append(out, cmds...)
	}
	return out, nil
}

// Target is the internal state a transform upserts into, keyed by command kind and target id.
type Target map[string]Command

// Upsert applies commands by (kind, target id) and returns how many changed the target.
// A command identical to the stored one is not a change, so rerunning a transform on the
// same snapshot returns zero.
//
// rule: MIG-008
func (t Target) Upsert(cmds []Command) (changed int, err error) {
	for _, c := range cmds {
		if c.Kind == "" || c.TargetID == "" {
			return changed, fmt.Errorf("upsert: command without kind or target id")
		}
		k := c.Kind + "\x00" + c.TargetID
		if prev, ok := t[k]; ok && reflect.DeepEqual(prev.Fields, c.Fields) {
			continue
		}
		t[k] = c
		changed++
	}
	return changed, nil
}

// DuplicateCandidate is a pair of staged rows that share a duplicate key. The importer only
// flags it; merging is a human action after import.
type DuplicateCandidate struct {
	Resource string
	Key      string
	A, B     string // external ids, A < B
}

// FlagDuplicates returns every pair of rows of the same resource whose key is equal.
// key returns "" for rows that cannot be compared. Keys are compared case-insensitively
// after trimming. Rows are never merged or dropped.
//
// rule: MIG-014
func FlagDuplicates(rows []Row, key func(Row) string) []DuplicateCandidate {
	groups := map[[2]string][]string{}
	for _, r := range rows {
		k := strings.ToLower(strings.TrimSpace(key(r)))
		if k == "" {
			continue
		}
		g := [2]string{r.Resource, k}
		groups[g] = append(groups[g], r.ExternalID)
	}
	var out []DuplicateCandidate
	for g, ids := range groups {
		sort.Strings(ids)
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				if ids[i] == ids[j] {
					continue
				}
				out = append(out, DuplicateCandidate{Resource: g[0], Key: g[1], A: ids[i], B: ids[j]})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.A != b.A {
			return a.A < b.A
		}
		return a.B < b.B
	})
	return out
}
