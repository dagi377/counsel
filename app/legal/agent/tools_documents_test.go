package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	listMatter  = "00000000-0000-4000-8000-000000000101"
	listWalled  = "00000000-0000-4000-8000-000000000102"
	listFolderA = "00000000-0000-4000-8000-000000000201"
	listFolderB = "00000000-0000-4000-8000-000000000202"
	listUnknown = "00000000-0000-4000-8000-000000000299"
)

type listDocsFake struct {
	folders map[string][]legaldocs.Folder
	docs    map[string][]agent.ListedDocument
}

func (f listDocsFake) Folders(_ context.Context, matterID string) ([]legaldocs.Folder, error) {
	return f.folders[matterID], nil
}

func (f listDocsFake) MatterDocuments(_ context.Context, matterID string) ([]agent.ListedDocument, error) {
	return f.docs[matterID], nil
}

func newListDocsRegistry(t *testing.T, actorCaps map[string]bool) *agenttools.Registry {
	t.Helper()
	walls := rolesfake.New()
	walls.Wall(clerk, listWalled)
	fake := listDocsFake{
		folders: map[string][]legaldocs.Folder{
			listMatter: {{ID: listFolderA, MatterID: listMatter, Name: "Pleadings"}, {ID: listFolderB, MatterID: listMatter, Name: "Correspondence"}},
			listWalled: {{ID: "f-w", MatterID: listWalled, Name: "Pleadings"}},
		},
		docs: map[string][]agent.ListedDocument{
			listMatter: {
				{Document: legaldocs.Document{ID: "doc-1", MatterID: listMatter, Kind: "pleading", FolderID: listFolderA}, VersionID: "v-1"},
				{Document: legaldocs.Document{ID: "doc-2", MatterID: listMatter, Kind: "letter", FolderID: listFolderB}, VersionID: "v-2"},
				{Document: legaldocs.Document{ID: "doc-3", MatterID: listMatter, Kind: "pleading", FolderID: listFolderA}, VersionID: "v-3"},
			},
			listWalled: {{Document: legaldocs.Document{ID: "doc-w", MatterID: listWalled, Kind: "memo", FolderID: "f-w"}, VersionID: "v-w"}},
		},
	}
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == clerk && actorCaps[capability], nil
	})
	h, ok := agent.HandlerFor(agent.ToolListDocuments)
	if !ok {
		t.Fatal("list_documents is missing from the handler set")
	}
	tool, err := h.Build(agent.NewDeps(can, walls, fake))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	reg := agenttools.NewRegistry(agenttoolsfake.New())
	if err := reg.Register(tool); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg
}

// rule: ARC-I-036
// rule: ARC-I-038
func TestARC_I_036_ListDocumentsCapabilityWallsAndSchema(t *testing.T) {
	granted := []string{agent.CapDocumentsView}
	cases := []struct {
		name      string
		grant     []string
		actorCaps map[string]bool
		in        string
		wantErr   error
		wantDocs  []string
		wantNext  string
	}{
		{"lists matter documents", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `"}`, nil, []string{"doc-1", "doc-2", "doc-3"}, ""},
		{"folder filter", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","folder_id":"` + listFolderA + `"}`, nil, []string{"doc-1", "doc-3"}, ""},
		{"first page", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","limit":2}`, nil, []string{"doc-1", "doc-2"}, "2"},
		{"second page", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","limit":2,"cursor":"2"}`, nil, []string{"doc-3"}, ""},
		{"walled matter is not found", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listWalled + `"}`, agent.ErrNotVisible, nil, ""},
		{"folder of another matter is not found", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","folder_id":"` + listUnknown + `"}`, agent.ErrNotVisible, nil, ""},
		{"agent grant lacks capability", nil, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `"}`, agenttools.ErrMissingCapability, nil, ""},
		{"acting user lacks capability", granted, map[string]bool{}, `{"matter_id":"` + listMatter + `"}`, agent.ErrForbidden, nil, ""},
		{"schema: matter_id required", granted, map[string]bool{agent.CapDocumentsView: true}, `{}`, agenttools.ErrInvalidInput, nil, ""},
		{"schema: extra field", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","bogus":1}`, agenttools.ErrInvalidInput, nil, ""},
		{"schema: limit too big", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","limit":51}`, agenttools.ErrInvalidInput, nil, ""},
		{"bad cursor", granted, map[string]bool{agent.CapDocumentsView: true}, `{"matter_id":"` + listMatter + `","cursor":"x"}`, agent.ErrInvalidInput, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := newListDocsRegistry(t, tc.actorCaps)
			p := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: clerk, Grant: tc.grant}
			res, err := reg.Call(context.Background(), p, agent.ToolListDocuments, json.RawMessage(tc.in))
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if err := agenttools.CheckCitations(res); err != nil {
				t.Fatalf("CheckCitations: %v", err)
			}
			var out struct {
				Documents []struct {
					DocID     string `json:"doc_id"`
					VersionID string `json:"version_id"`
					Folder    string `json:"folder"`
				} `json:"documents"`
				NextCursor string `json:"next_cursor"`
			}
			if err := json.Unmarshal(res.Output, &out); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, d := range out.Documents {
				if d.VersionID == "" || d.Folder == "" {
					t.Errorf("document %s: version %q folder %q", d.DocID, d.VersionID, d.Folder)
				}
				got = append(got, d.DocID)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.wantDocs) || out.NextCursor != tc.wantNext {
				t.Fatalf("docs = %v next %q, want %v next %q", got, out.NextCursor, tc.wantDocs, tc.wantNext)
			}
		})
	}
}

func TestListDocumentsBuildNeedsDeps(t *testing.T) {
	h, ok := agent.HandlerFor(agent.ToolListDocuments)
	if !ok {
		t.Fatal("no handler")
	}
	cases := []struct {
		name string
		deps agent.Deps
	}{
		{"no deps", agent.NewDeps()},
		{"walls only", agent.NewDeps(rolesfake.New())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.Build(tc.deps); !errors.Is(err, agent.ErrMissingDep) {
				t.Fatalf("err = %v, want ErrMissingDep", err)
			}
		})
	}
}
