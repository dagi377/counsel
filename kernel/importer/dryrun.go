package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// UnmappedError is the error a Transform returns for a resource or field it has no mapping
// for. An empty Field means the whole resource has no import path. It matches ErrUnmapped.
type UnmappedError struct {
	Resource string
	Field    string
}

func (e *UnmappedError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: resource %s", ErrUnmapped, e.Resource)
	}
	return fmt.Sprintf("%s: field %s.%s", ErrUnmapped, e.Resource, e.Field)
}

// Is makes errors.Is(err, ErrUnmapped) true.
func (e *UnmappedError) Is(target error) bool { return target == ErrUnmapped }

// FieldMapper is optionally implemented by a Source that declares which fields of a resource
// it maps. Any other field on a staged row is reported as an unmapped field.
type FieldMapper interface {
	MappedFields(resource string) []string
}

// SnapshotSHA returns the snapshot id of a staging set: the hex sha256 of its sorted page
// sha256s. A new or changed staged page changes the snapshot, which invalidates approval.
//
// rule: MIG-015
func SnapshotSHA(pageSHAs []string) string {
	s := append([]string(nil), pageSHAs...)
	sort.Strings(s)
	sum := sha256.Sum256([]byte(strings.Join(s, "\n")))
	return hex.EncodeToString(sum[:])
}

// BuildDryRun transforms the staged rows of one snapshot into a scratch id map and reports
// row counts, unmapped resources and fields with counts, and errors. It writes nothing.
// An unmapped resource or field is listed, not an error; any other transform failure is.
//
// rule: MIG-012
// rule: MIG-013
func BuildDryRun(ctx context.Context, sourceID, snapshot string, s Source, rows []Row) (DryRun, error) {
	if sourceID == "" || snapshot == "" || s == nil {
		return DryRun{}, fmt.Errorf("dry run: source id, snapshot and source are required")
	}
	d := DryRun{Source: sourceID, Snapshot: snapshot, Counts: map[string]int{}, Unmapped: map[string]int{}}
	scratch := NewMemIDMap()
	sorted := append([]Row(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Resource != sorted[j].Resource {
			return sorted[i].Resource < sorted[j].Resource
		}
		return sorted[i].ExternalID < sorted[j].ExternalID
	})
	for _, r := range sorted {
		if _, err := scratch.Assign(ctx, sourceID, r.Resource, r.ExternalID); err != nil {
			d.Errors = append(d.Errors, Issue{Resource: r.Resource, ExternalID: r.ExternalID, Message: err.Error()})
		}
	}
	fm, _ := s.(FieldMapper)
	mapped := map[string]map[string]bool{}
	for _, r := range sorted {
		if err := ctx.Err(); err != nil {
			return DryRun{}, fmt.Errorf("dry run %s: %w", sourceID, err)
		}
		d.Counts[r.Resource]++
		// rule: MIG-013
		if fm != nil {
			known, ok := mapped[r.Resource]
			if !ok {
				known = map[string]bool{}
				for _, f := range fm.MappedFields(r.Resource) {
					known[f] = true
				}
				mapped[r.Resource] = known
			}
			for f := range r.Fields {
				if !known[f] {
					d.Unmapped[r.Resource+"."+f]++
				}
			}
		}
		_, err := s.Transform(ctx, r, scratch)
		var ue *UnmappedError
		switch {
		case err == nil:
		case errors.As(err, &ue):
			// rule: MIG-012
			// rule: MIG-013
			key := ue.Resource
			if key == "" {
				key = r.Resource
			}
			if ue.Field != "" {
				key += "." + ue.Field
			}
			d.Unmapped[key]++
		case errors.Is(err, ErrUnmapped):
			d.Unmapped[r.Resource]++
		default:
			d.Errors = append(d.Errors, Issue{Resource: r.Resource, ExternalID: r.ExternalID, Message: err.Error()})
		}
	}
	return d, nil
}

// ImportGate is the import gate (G-IMP step "Import"). Approve returns nil only once the
// owner approved the dry run; the app wires it to the kernel gate engine.
type ImportGate interface {
	Approve(ctx context.Context, d DryRun) error
}

// DryRuns keeps dry runs and their approvals. It is safe for concurrent use.
type DryRuns struct {
	mu   sync.Mutex
	seq  int
	byID map[string]DryRun
	gate ImportGate
}

// NewDryRuns returns an empty dry-run store whose approvals go through gate.
func NewDryRuns(gate ImportGate) *DryRuns {
	return &DryRuns{byID: map[string]DryRun{}, gate: gate}
}

// Record stores d unapproved under a new id and returns it.
func (r *DryRuns) Record(d DryRun) DryRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	d.ID = "dryrun-" + strconv.Itoa(r.seq)
	d.Approved = false
	r.byID[d.ID] = d
	return d
}

// Approve marks a zero-error dry run approved once the import gate approves it.
//
// rule: MIG-015
func (r *DryRuns) Approve(ctx context.Context, id string) error {
	r.mu.Lock()
	d, ok := r.byID[id]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("approve dry run %q: %w", id, ErrNoApprovedDryRun)
	}
	if len(d.Errors) > 0 {
		return fmt.Errorf("approve dry run %q: %d errors: %w", id, len(d.Errors), ErrDryRunHasErrors)
	}
	if r.gate == nil {
		return fmt.Errorf("approve dry run %q: no import gate: %w", id, ErrNoApprovedDryRun)
	}
	if err := r.gate.Approve(ctx, d); err != nil {
		return fmt.Errorf("approve dry run %q: import gate: %w", id, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d.Approved = true
	r.byID[id] = d
	return nil
}

// RequireApproved returns nil only when an approved, zero-error dry run exists for the same
// source and snapshot. Run calls it before importing anything.
//
// rule: MIG-015
func (r *DryRuns) RequireApproved(source, snapshot string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.byID {
		if d.Approved && len(d.Errors) == 0 && d.Source == source && d.Snapshot == snapshot {
			return nil
		}
	}
	return fmt.Errorf("run %s snapshot %s: %w", source, snapshot, ErrNoApprovedDryRun)
}
