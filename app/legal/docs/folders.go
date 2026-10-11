package docs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// folderNode is one folder of an expanded template: its full path and its parent's path.
type folderNode struct {
	Path       string
	ParentPath string
	Name       string
}

// expandTemplate turns slash-separated template paths into folder nodes, parents before
// children, each folder once. "Pleadings/Drafts" yields "Pleadings" then "Pleadings/Drafts".
func expandTemplate(paths []string) ([]folderNode, error) {
	seen := map[string]bool{}
	var out []folderNode
	for _, p := range paths {
		parts := strings.Split(p, "/")
		for i, name := range parts {
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, fmt.Errorf("template path %q: empty segment: %w", p, ErrInvalid)
			}
			parts[i] = name
			path := strings.Join(parts[:i+1], "/")
			if seen[strings.ToLower(path)] {
				continue
			}
			seen[strings.ToLower(path)] = true
			out = append(out, folderNode{Path: path, ParentPath: strings.Join(parts[:i], "/"), Name: name})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("template: no folders: %w", ErrInvalid)
	}
	return out, nil
}

// FolderStore keeps matter folder trees in docs.folder and files documents by setting
// docs.document.folder_id. A document has one folder_id, so it lives in exactly one folder.
type FolderStore struct {
	db        db.DB
	templates map[string][]string
}

var _ Folders = (*FolderStore)(nil)

// NewFolderStore returns a store on pool with one folder template per practice area.
func NewFolderStore(pool db.DB, templates []FolderTemplate) (*FolderStore, error) {
	t := map[string][]string{}
	for _, ft := range templates {
		if ft.PracticeArea == "" {
			return nil, fmt.Errorf("folder template: practice area: %w", ErrInvalid)
		}
		if _, err := expandTemplate(ft.Paths); err != nil {
			return nil, fmt.Errorf("folder template %s: %w", ft.PracticeArea, err)
		}
		t[ft.PracticeArea] = append([]string(nil), ft.Paths...)
	}
	return &FolderStore{db: pool, templates: t}, nil
}

// CreateMatterFolders seeds a new matter's tree from its practice area's template. If the
// matter already has folders, it returns them and creates nothing.
func (s *FolderStore) CreateMatterFolders(ctx context.Context, tx pgx.Tx, matterID, practiceArea string) ([]Folder, error) {
	if matterID == "" {
		return nil, fmt.Errorf("create folders: matter: %w", ErrInvalid)
	}
	existing, err := listFolders(ctx, tx, matterID)
	if err != nil {
		return nil, fmt.Errorf("create folders: %w", err)
	}
	if len(existing) > 0 {
		return existing, nil
	}
	paths, ok := s.templates[practiceArea]
	if !ok {
		return nil, fmt.Errorf("create folders %q: %w", practiceArea, ErrNoTemplate)
	}
	nodes, err := expandTemplate(paths)
	if err != nil {
		return nil, fmt.Errorf("create folders: %w", err)
	}
	ids := map[string]FolderID{}
	out := make([]Folder, 0, len(nodes))
	for _, n := range nodes {
		parent := ids[strings.ToLower(n.ParentPath)]
		var parentArg any
		if parent != "" {
			parentArg = string(parent)
		}
		var id string
		err := tx.QueryRow(ctx,
			`INSERT INTO docs.folder (matter_id, parent_id, name, from_template)
			 VALUES ($1, $2, $3, true) RETURNING id::text`,
			matterID, parentArg, n.Name).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("create folders: insert: %w", err)
		}
		ids[strings.ToLower(n.Path)] = FolderID(id)
		out = append(out, Folder{ID: FolderID(id), MatterID: matterID, ParentID: parent, Name: n.Name})
	}
	return out, nil
}

// Folders lists a matter's live folders, parents first.
func (s *FolderStore) Folders(ctx context.Context, matterID string) ([]Folder, error) {
	var out []Folder
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = listFolders(ctx, tx, matterID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("folders: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("folders %s: %w", matterID, ErrNotFound)
	}
	return out, nil
}

// listFolders reads a matter's live folders and orders them parents first.
func listFolders(ctx context.Context, tx pgx.Tx, matterID string) ([]Folder, error) {
	rows, err := tx.Query(ctx,
		`SELECT id::text, coalesce(parent_id::text, ''), name FROM docs.folder
		 WHERE matter_id = $1 AND archived_at IS NULL ORDER BY id`, matterID)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	var fs []Folder
	for rows.Next() {
		f := Folder{MatterID: matterID}
		var id, parent string
		if err := rows.Scan(&id, &parent, &f.Name); err != nil {
			return nil, fmt.Errorf("list folders: scan: %w", err)
		}
		f.ID, f.ParentID = FolderID(id), FolderID(parent)
		fs = append(fs, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	return parentsFirst(fs), nil
}

// parentsFirst orders folders so every parent comes before its children, keeping the input
// order among siblings. Folders whose parent is missing are treated as top level.
func parentsFirst(fs []Folder) []Folder {
	present := map[FolderID]bool{}
	children := map[FolderID][]Folder{}
	for _, f := range fs {
		present[f.ID] = true
	}
	for _, f := range fs {
		p := f.ParentID
		if !present[p] {
			p = ""
		}
		children[p] = append(children[p], f)
	}
	out := make([]Folder, 0, len(fs))
	var walk func(FolderID)
	walk = func(id FolderID) {
		for _, c := range children[id] {
			out = append(out, c)
			walk(c.ID)
		}
	}
	walk("")
	return out
}

// checkFiling validates a filing against the target folder's matter.
func checkFiling(d Document, folderMatter string, known func(kind string) bool) error {
	if d.ID == "" || d.MatterID == "" || d.FolderID == "" {
		return fmt.Errorf("file: %w", ErrInvalid)
	}
	if !known(d.Kind) {
		return fmt.Errorf("file %s: %w", d.Kind, ErrUnknownKind)
	}
	if folderMatter != d.MatterID {
		return fmt.Errorf("file: %w", ErrWrongMatter)
	}
	return nil
}

// knownKind reports whether kind is a registered legal document kind.
func knownKind(kind string) bool {
	for _, k := range documentKinds {
		if k.ID == kind {
			return true
		}
	}
	return false
}

// File puts a document in exactly one folder of its matter. Filing again moves it.
func (s *FolderStore) File(ctx context.Context, tx pgx.Tx, d Document) error {
	var folderMatter string
	if d.FolderID != "" {
		err := tx.QueryRow(ctx,
			`SELECT coalesce(matter_id::text, '') FROM docs.folder WHERE id = $1 AND archived_at IS NULL`,
			string(d.FolderID)).Scan(&folderMatter)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("file: folder: %w", ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("file: folder: %w", err)
		}
	}
	if err := checkFiling(d, folderMatter, knownKind); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`UPDATE docs.document SET folder_id = $1, updated_at = now(), row_version = row_version + 1
		 WHERE id = $2 AND matter_id = $3 AND doc_type = $4 AND archived_at IS NULL`,
		string(d.FolderID), d.ID, d.MatterID, d.Kind)
	if err != nil {
		return fmt.Errorf("file: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("file: document: %w", ErrNotFound)
	}
	return nil
}

// Document returns one filed document.
func (s *FolderStore) Document(ctx context.Context, id string) (Document, error) {
	var d Document
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var folder string
		err := tx.QueryRow(ctx,
			`SELECT id::text, coalesce(matter_id::text, ''), doc_type, coalesce(folder_id::text, '')
			 FROM docs.document WHERE id = $1 AND archived_at IS NULL`, id).
			Scan(&d.ID, &d.MatterID, &d.Kind, &folder)
		d.FolderID = FolderID(folder)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, fmt.Errorf("document %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Document{}, fmt.Errorf("document %s: %w", id, err)
	}
	return d, nil
}
