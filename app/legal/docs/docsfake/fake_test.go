package docsfake

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/jackc/pgx/v5"
)

type gates struct {
	opened [][]string
	kind   string
}

func (g *gates) OpenDestruction(_ context.Context, _ pgx.Tx, _ string, ids []string, _ string) (string, error) {
	g.opened = append(g.opened, ids)
	g.kind = docs.DestructionGate
	return "gate-1", nil
}

func newFake(g docs.DestructionGates) *Fake {
	return New(
		[]docs.DocumentKind{
			{ID: docs.KindEngagementLetter, RetentionClass: "RC-FILE"},
			{ID: docs.KindCourtForm, RetentionClass: "RC-FILE"},
			{ID: "invoice_copy", RetentionClass: docs.RetentionFinancial},
		},
		[]docs.RetentionClass{
			{ID: "RC-FILE", MinYears: 10, DestructionGate: docs.DestructionGate},
			{ID: docs.RetentionFinancial, NeverPurge: true},
		},
		[]docs.FolderTemplate{{PracticeArea: "litigation", Paths: []string{"Pleadings/Drafts", "Pleadings/Filed", "Correspondence"}}},
		g,
	)
}

func TestCreateMatterFolders(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		area      string
		want      error
		wantNames []string
	}{
		{name: "template tree", area: "litigation", wantNames: []string{"Pleadings", "Drafts", "Filed", "Correspondence"}},
		{name: "no template", area: "tax", want: docs.ErrNoTemplate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(&gates{})
			fs, err := f.CreateMatterFolders(ctx, nil, "m1", tt.area)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			var names []string
			for _, fo := range fs {
				names = append(names, fo.Name)
			}
			if !slices.Equal(names, tt.wantNames) {
				t.Fatalf("names = %v, want %v", names, tt.wantNames)
			}
			if err == nil {
				if fs[1].ParentID != fs[0].ID || fs[0].ParentID != "" {
					t.Fatalf("parents wrong: %+v", fs)
				}
				again, _ := f.CreateMatterFolders(ctx, nil, "m1", tt.area)
				if !slices.Equal(again, fs) {
					t.Fatal("second call built a new tree")
				}
			}
		})
	}
}

func TestFile(t *testing.T) {
	ctx := context.Background()
	f := newFake(&gates{})
	m1, _ := f.CreateMatterFolders(ctx, nil, "m1", "litigation")
	m2, _ := f.CreateMatterFolders(ctx, nil, "m2", "litigation")
	tests := []struct {
		name string
		d    docs.Document
		want error
	}{
		{name: "filed", d: docs.Document{ID: "d1", MatterID: "m1", Kind: docs.KindCourtForm, FolderID: m1[1].ID}},
		{name: "moved keeps one folder", d: docs.Document{ID: "d1", MatterID: "m1", Kind: docs.KindCourtForm, FolderID: m1[2].ID}},
		{name: "folder of other matter", d: docs.Document{ID: "d2", MatterID: "m1", Kind: docs.KindCourtForm, FolderID: m2[0].ID}, want: docs.ErrWrongMatter},
		{name: "unknown folder", d: docs.Document{ID: "d2", MatterID: "m1", Kind: docs.KindCourtForm, FolderID: "nope"}, want: docs.ErrNotFound},
		{name: "unknown kind", d: docs.Document{ID: "d2", MatterID: "m1", Kind: "memo", FolderID: m1[0].ID}, want: docs.ErrUnknownKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.File(ctx, nil, tt.d); !errors.Is(err, tt.want) {
				t.Fatalf("File err = %v, want %v", err, tt.want)
			}
			if tt.want == nil {
				if d, _ := f.Document(ctx, tt.d.ID); d.FolderID != tt.d.FolderID {
					t.Fatalf("document in %s, want %s", d.FolderID, tt.d.FolderID)
				}
			}
		})
	}
}

func TestProposeDestruction(t *testing.T) {
	ctx := context.Background()
	closed := time.Date(2016, 3, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		closed  time.Time
		today   time.Time
		want    error
		wantIDs []string
	}{
		{name: "retention ran out", closed: closed, today: closed.AddDate(10, 0, 0), wantIDs: []string{"d1", "d2"}},
		{name: "one day early", closed: closed, today: closed.AddDate(10, 0, -1), want: docs.ErrNothingToPurge},
		{name: "matter not closed", today: closed, want: docs.ErrMatterOpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gates{}
			f := newFake(g)
			fs, _ := f.CreateMatterFolders(ctx, nil, "m1", "litigation")
			for _, d := range []docs.Document{
				{ID: "d1", MatterID: "m1", Kind: docs.KindCourtForm, FolderID: fs[0].ID},
				{ID: "d2", MatterID: "m1", Kind: docs.KindEngagementLetter, FolderID: fs[0].ID},
				{ID: "d3", MatterID: "m1", Kind: "invoice_copy", FolderID: fs[0].ID},
			} {
				if err := f.File(ctx, nil, d); err != nil {
					t.Fatal(err)
				}
			}
			p, err := f.ProposeDestruction(ctx, nil, "m1", tt.closed, tt.today, "clerk")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if !slices.Equal(p.DocumentIDs, tt.wantIDs) {
				t.Fatalf("ids = %v, want %v", p.DocumentIDs, tt.wantIDs)
			}
			if err == nil && (len(g.opened) != 1 || g.kind != docs.DestructionGate || p.GateID != "gate-1") {
				t.Fatalf("gate not opened once: %+v", g)
			}
			if err != nil && len(g.opened) != 0 {
				t.Fatal("gate opened on error")
			}
		})
	}
}
