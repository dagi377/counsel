// Package docsfake is an in-memory docs.Service for tests: no network, Postgres or Docker.
package docsfake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/jackc/pgx/v5"
)

var _ docs.Service = (*Fake)(nil)

// Fake is an in-memory legal docs service.
type Fake struct {
	mu        sync.Mutex
	kinds     []docs.DocumentKind
	classes   []docs.RetentionClass
	templates map[string][]string
	gates     docs.DestructionGates
	folders   map[string][]docs.Folder // matter id -> folders, parents first
	documents map[string]docs.Document
	seq       int
}

// New returns a service with the given catalog, folder templates and gate opener.
func New(kinds []docs.DocumentKind, classes []docs.RetentionClass, templates []docs.FolderTemplate, gates docs.DestructionGates) *Fake {
	f := &Fake{
		kinds: slices.Clone(kinds), classes: slices.Clone(classes), templates: map[string][]string{},
		gates: gates, folders: map[string][]docs.Folder{}, documents: map[string]docs.Document{},
	}
	for _, t := range templates {
		f.templates[t.PracticeArea] = slices.Clone(t.Paths)
	}
	return f
}

// Kinds lists the document kinds.
func (f *Fake) Kinds() []docs.DocumentKind { return slices.Clone(f.kinds) }

// RetentionClasses lists the retention classes.
func (f *Fake) RetentionClasses() []docs.RetentionClass { return slices.Clone(f.classes) }

// CreateMatterFolders seeds a matter's folder tree from its practice area template.
func (f *Fake) CreateMatterFolders(_ context.Context, _ pgx.Tx, matterID, practiceArea string) ([]docs.Folder, error) {
	if matterID == "" {
		return nil, fmt.Errorf("create folders: %w", docs.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.folders[matterID]; ok {
		return slices.Clone(existing), nil
	}
	paths, ok := f.templates[practiceArea]
	if !ok {
		return nil, fmt.Errorf("create folders %q: %w", practiceArea, docs.ErrNoTemplate)
	}
	byPath := map[string]docs.FolderID{}
	var out []docs.Folder
	for _, p := range paths {
		var parent docs.FolderID
		parts := strings.Split(p, "/")
		for i := range parts {
			path := strings.Join(parts[:i+1], "/")
			if id, ok := byPath[path]; ok {
				parent = id
				continue
			}
			f.seq++
			fo := docs.Folder{ID: docs.FolderID(fmt.Sprintf("folder-%d", f.seq)), MatterID: matterID, ParentID: parent, Name: parts[i]}
			byPath[path] = fo.ID
			out = append(out, fo)
			parent = fo.ID
		}
	}
	f.folders[matterID] = out
	return slices.Clone(out), nil
}

// Folders lists a matter's folders.
func (f *Fake) Folders(_ context.Context, matterID string) ([]docs.Folder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fs, ok := f.folders[matterID]
	if !ok {
		return nil, fmt.Errorf("folders %s: %w", matterID, docs.ErrNotFound)
	}
	return slices.Clone(fs), nil
}

// File puts a document in one folder of its matter.
func (f *Fake) File(_ context.Context, _ pgx.Tx, d docs.Document) error {
	if d.ID == "" || d.MatterID == "" {
		return fmt.Errorf("file: %w", docs.ErrInvalid)
	}
	if !slices.ContainsFunc(f.kinds, func(k docs.DocumentKind) bool { return k.ID == d.Kind }) {
		return fmt.Errorf("file %s: %w", d.Kind, docs.ErrUnknownKind)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, fs := range f.folders {
		for _, fo := range fs {
			if fo.ID == d.FolderID {
				found = true
				if fo.MatterID != d.MatterID {
					return fmt.Errorf("file: %w", docs.ErrWrongMatter)
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("file: folder: %w", docs.ErrNotFound)
	}
	f.documents[d.ID] = d
	return nil
}

// Document returns one filed document.
func (f *Fake) Document(_ context.Context, id string) (docs.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.documents[id]
	if !ok {
		return docs.Document{}, fmt.Errorf("document %s: %w", id, docs.ErrNotFound)
	}
	return d, nil
}

// ProposeDestruction opens the destruction gate for documents past retention.
func (f *Fake) ProposeDestruction(ctx context.Context, tx pgx.Tx, matterID string, closedOn, today time.Time, preparer string) (docs.Proposal, error) {
	if closedOn.IsZero() || closedOn.After(today) {
		return docs.Proposal{}, fmt.Errorf("propose destruction %s: %w", matterID, docs.ErrMatterOpen)
	}
	f.mu.Lock()
	classOf := map[string]docs.RetentionClass{}
	for _, k := range f.kinds {
		for _, c := range f.classes {
			if c.ID == k.RetentionClass {
				classOf[k.ID] = c
			}
		}
	}
	var ids []string
	for _, d := range f.documents {
		c, ok := classOf[d.Kind]
		if d.MatterID != matterID || !ok || c.NeverPurge {
			continue
		}
		if !closedOn.AddDate(c.MinYears, 0, 0).After(today) {
			ids = append(ids, d.ID)
		}
	}
	f.mu.Unlock()
	if len(ids) == 0 {
		return docs.Proposal{}, fmt.Errorf("propose destruction %s: %w", matterID, docs.ErrNothingToPurge)
	}
	slices.Sort(ids)
	gateID, err := f.gates.OpenDestruction(ctx, tx, matterID, ids, preparer)
	if err != nil {
		return docs.Proposal{}, fmt.Errorf("propose destruction %s: open gate: %w", matterID, err)
	}
	return docs.Proposal{MatterID: matterID, GateID: gateID, DocumentIDs: ids, ProposedOn: today}, nil
}
