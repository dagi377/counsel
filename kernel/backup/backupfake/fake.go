// Package backupfake is an in-memory kernel/backup for tests: no network, no bucket, no
// Postgres.
package backupfake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/backup"
)

var (
	_ backup.ObjectStore   = (*Fake)(nil)
	_ backup.Exporter      = (*Fake)(nil)
	_ backup.RestoreChecks = (*Fake)(nil)
)

type check struct {
	name string
	fn   backup.CheckFunc
}

// Fake holds objects, export records and restore checks in memory.
type Fake struct {
	mu      sync.Mutex
	bucket  string
	objects map[string]backup.ObjectRef
	exports map[backup.Month]backup.ExportRecord
	checks  []check
	// FailPut makes the next Put fail, to test that no record is written without its object.
	FailPut error
}

// New returns an empty fake for bucket.
func New(bucket string) *Fake {
	return &Fake{bucket: bucket, objects: map[string]backup.ObjectRef{}, exports: map[backup.Month]backup.ExportRecord{}}
}

// Put stores r under key.
func (f *Fake) Put(_ context.Context, key string, r io.Reader) (backup.ObjectRef, error) {
	f.mu.Lock()
	failure := f.FailPut
	f.FailPut = nil
	f.mu.Unlock()
	if failure != nil {
		return backup.ObjectRef{}, fmt.Errorf("put %s: %w", key, failure)
	}
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return backup.ObjectRef{}, fmt.Errorf("put %s: %w", key, err)
	}
	ref := backup.ObjectRef{Bucket: f.bucket, Key: key, SHA256: hex.EncodeToString(h.Sum(nil)), Size: n}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = ref
	return ref, nil
}

// Head returns an object's metadata.
func (f *Fake) Head(_ context.Context, key string) (backup.ObjectRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref, ok := f.objects[key]
	if !ok {
		return backup.ObjectRef{}, fmt.Errorf("head %s: %w", key, backup.ErrNotFound)
	}
	return ref, nil
}

// ExportMonth puts the object, confirms it with Head, then records the export.
func (f *Fake) ExportMonth(ctx context.Context, m backup.Month, r io.Reader) (backup.ExportRecord, error) {
	f.mu.Lock()
	_, done := f.exports[m]
	f.mu.Unlock()
	if done {
		return backup.ExportRecord{}, fmt.Errorf("export %d-%02d: %w", m.Year, m.Month, backup.ErrAlreadyExported)
	}
	key := fmt.Sprintf("month-end/%04d-%02d.tar", m.Year, m.Month)
	if _, err := f.Put(ctx, key, r); err != nil {
		return backup.ExportRecord{}, err
	}
	ref, err := f.Head(ctx, key)
	if err != nil {
		return backup.ExportRecord{}, fmt.Errorf("export %s: %w", key, backup.ErrObjectMissing)
	}
	rec := backup.ExportRecord{Month: m, Object: ref}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exports[m] = rec
	return rec, nil
}

// Export returns the recorded export for m.
func (f *Fake) Export(_ context.Context, m backup.Month) (backup.ExportRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.exports[m]
	if !ok {
		return backup.ExportRecord{}, fmt.Errorf("export %d-%02d: %w", m.Year, m.Month, backup.ErrNotFound)
	}
	return rec, nil
}

// Register adds a named restore check.
func (f *Fake) Register(name string, fn backup.CheckFunc) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.checks {
		if c.name == name {
			return fmt.Errorf("register %s: %w", name, backup.ErrDuplicateCheck)
		}
	}
	f.checks = append(f.checks, check{name: name, fn: fn})
	return nil
}

// Run runs every check in registration order; any failure returns ErrCheckFailed.
func (f *Fake) Run(ctx context.Context, tx pgx.Tx) ([]backup.CheckResult, error) {
	f.mu.Lock()
	checks := append([]check(nil), f.checks...)
	f.mu.Unlock()
	var out []backup.CheckResult
	var failed bool
	for _, c := range checks {
		err := c.fn(ctx, tx)
		failed = failed || err != nil
		out = append(out, backup.CheckResult{Name: c.name, Err: err})
	}
	if failed {
		return out, fmt.Errorf("restore checks: %w", backup.ErrCheckFailed)
	}
	return out, nil
}
