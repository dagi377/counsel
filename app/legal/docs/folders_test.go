package docs

import (
	"errors"
	"slices"
	"testing"
)

func TestExpandTemplateGivesDefaultTreeParentsFirst(t *testing.T) {
	cases := []struct {
		name    string
		paths   []string
		want    []folderNode
		wantErr error
	}{
		{
			name:  "nested paths create parents once",
			paths: []string{"Pleadings/Drafts", "Pleadings/Filed", "Correspondence"},
			want: []folderNode{
				{Path: "Pleadings", ParentPath: "", Name: "Pleadings"},
				{Path: "Pleadings/Drafts", ParentPath: "Pleadings", Name: "Drafts"},
				{Path: "Pleadings/Filed", ParentPath: "Pleadings", Name: "Filed"},
				{Path: "Correspondence", ParentPath: "", Name: "Correspondence"},
			},
		},
		{
			name:  "duplicate path differing in case is one folder",
			paths: []string{"Evidence", "evidence"},
			want:  []folderNode{{Path: "Evidence", Name: "Evidence"}},
		},
		{name: "empty segment rejected", paths: []string{"Pleadings//Drafts"}, wantErr: ErrInvalid},
		{name: "empty template rejected", paths: nil, wantErr: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandTemplate(tc.paths)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("nodes = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNewFolderStoreValidatesTemplates(t *testing.T) {
	cases := []struct {
		name      string
		templates []FolderTemplate
		wantErr   error
	}{
		{name: "valid", templates: []FolderTemplate{{PracticeArea: "litigation", Paths: []string{"Pleadings"}}}},
		{name: "missing practice area", templates: []FolderTemplate{{Paths: []string{"Pleadings"}}}, wantErr: ErrInvalid},
		{name: "empty tree", templates: []FolderTemplate{{PracticeArea: "litigation"}}, wantErr: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewFolderStore(nil, tc.templates)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestParentsFirstOrdersTree(t *testing.T) {
	in := []Folder{
		{ID: "c", ParentID: "a", Name: "Drafts"},
		{ID: "a", Name: "Pleadings"},
		{ID: "b", Name: "Correspondence"},
		{ID: "d", ParentID: "gone", Name: "Orphan"},
	}
	got := parentsFirst(in)
	var ids []FolderID
	for _, f := range got {
		ids = append(ids, f.ID)
	}
	want := []FolderID{"a", "c", "b", "d"}
	if !slices.Equal(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
}

func TestCheckFilingOneFolderOfOwnMatter(t *testing.T) {
	doc := Document{ID: "d1", MatterID: "m1", Kind: KindCourtForm, FolderID: "f1"}
	cases := []struct {
		name         string
		doc          Document
		folderMatter string
		wantErr      error
	}{
		{name: "folder of same matter", doc: doc, folderMatter: "m1"},
		{name: "folder of another matter", doc: doc, folderMatter: "m2", wantErr: ErrWrongMatter},
		{name: "no folder", doc: Document{ID: "d1", MatterID: "m1", Kind: KindCourtForm}, folderMatter: "m1", wantErr: ErrInvalid},
		{name: "no matter", doc: Document{ID: "d1", Kind: KindCourtForm, FolderID: "f1"}, folderMatter: "", wantErr: ErrInvalid},
		{name: "unknown kind", doc: Document{ID: "d1", MatterID: "m1", Kind: "x", FolderID: "f1"}, folderMatter: "m1", wantErr: ErrUnknownKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkFiling(tc.doc, tc.folderMatter, knownKind)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
