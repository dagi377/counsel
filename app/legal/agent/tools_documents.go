package agent

import (
	"context"
	"encoding/json"
	"fmt"

	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolListDocuments lists the filed documents of one matter.
const ToolListDocuments = "list_documents"

// MatterFolders lists a matter's folders, parents first (app/legal/docs Folders).
type MatterFolders interface {
	Folders(ctx context.Context, matterID string) ([]legaldocs.Folder, error)
}

func init() {
	addHandler(Handler{Name: ToolListDocuments, Build: buildListDocuments})
}

// buildListDocuments reads Deps: CapabilityCheck, roles.Walls, MatterFolders, MatterDocuments.
func buildListDocuments(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	folders, err := DepOf[MatterFolders](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	docs, err := DepOf[MatterDocuments](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	l := listDocuments{walls: walls, folders: folders, docs: docs}
	return handlerTool(ToolListDocuments, Read, can, l.handle)
}

type listDocuments struct {
	walls   roles.Walls
	folders MatterFolders
	docs    MatterDocuments
}

// listedDocumentOut is one listed document. It carries ids, kind and folder only: folder
// names come from the practice-area template, and no document text is listed.
type listedDocumentOut struct {
	DocID     string `json:"doc_id"`
	VersionID string `json:"version_id"`
	Kind      string `json:"kind"`
	FolderID  string `json:"folder_id"`
	Folder    string `json:"folder"`
}

type listDocumentsOut struct {
	Documents  []listedDocumentOut `json:"documents"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (l listDocuments) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		MatterID string `json:"matter_id"`
		FolderID string `json:"folder_id"`
		Cursor   string `json:"cursor"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDocuments, ErrInvalidInput)
	}
	offset, n, err := pageWindow(in.Cursor, in.Limit)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	// A walled matter is not found, not forbidden.
	if err := visibleTo(ctx, l.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	folders, err := l.folders.Folders(ctx, in.MatterID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: folders: %w", ToolListDocuments, err)
	}
	names := make(map[legaldocs.FolderID]string, len(folders))
	for _, f := range folders {
		names[f.ID] = f.Name
	}
	if in.FolderID != "" {
		if _, ok := names[legaldocs.FolderID(in.FolderID)]; !ok {
			return registry.ToolResult{}, fmt.Errorf("%s: folder: %w", ToolListDocuments, ErrNotVisible)
		}
	}
	all, err := l.docs.MatterDocuments(ctx, in.MatterID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: documents: %w", ToolListDocuments, err)
	}
	var match []listedDocumentOut
	for _, ld := range all {
		doc := ld.Document
		if doc.MatterID != in.MatterID || (in.FolderID != "" && string(doc.FolderID) != in.FolderID) {
			continue
		}
		match = append(match, listedDocumentOut{
			DocID: doc.ID, VersionID: string(ld.VersionID), Kind: doc.Kind,
			FolderID: string(doc.FolderID), Folder: names[doc.FolderID],
		})
	}
	out := listDocumentsOut{Documents: []listedDocumentOut{}, NextCursor: nextCursor(offset, n, len(match))}
	if offset < len(match) {
		out.Documents = append(out.Documents, match[offset:min(offset+n, len(match))]...)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListDocuments, err)
	}
	return registry.ToolResult{Output: b}, nil
}
