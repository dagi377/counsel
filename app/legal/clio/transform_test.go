package clio_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/clio"
	"github.com/dagi377/counsel/app/legal/clio/cliofake"
	"github.com/dagi377/counsel/kernel/importer"
)

func seeded() *cliofake.Fake {
	f := cliofake.New(2)
	f.Seed(clio.ResourceContacts,
		json.RawMessage(`{"id": 101, "type": "Person", "name": "Ada Li", "primary_email_address": "a@x.ca"}`),
		json.RawMessage(`{"id": 102, "type": "Company", "name": "Acme Ltd"}`),
		json.RawMessage(`{"id": 103, "type": "Person", "name": "Bo Chen"}`))
	f.Seed(clio.ResourceMatters,
		json.RawMessage(`{"id": 201, "display_number": "00001-Li", "description": "Lease", "status": "Open", "client": {"id": 101}}`))
	f.Seed(clio.ResourceActivities,
		json.RawMessage(`{"id": 301, "type": "TimeEntry", "date": "2026-10-01", "quantity": 3600, "price": 350.10, "matter": {"id": 201}}`))
	f.Seed(clio.ResourceBills,
		json.RawMessage(`{"id": 401, "number": "B-1", "issued_at": "2026-10-05", "state": "paid", "total": 1234.50, "currency": "CAD", "matter": {"id": 201}}`))
	return f
}

func extractAll(t *testing.T, e *clio.Extractor) []importer.Row {
	t.Helper()
	var rows []importer.Row
	for _, res := range e.Resources() {
		cur := importer.Cursor("")
		for {
			p, err := e.Extract(context.Background(), res, cur)
			if err != nil {
				t.Fatalf("Extract %s: %v", res, err)
			}
			rows = append(rows, p.Rows...)
			if p.Done {
				break
			}
			cur = p.Next
		}
	}
	return rows
}

func TestTransformRerunZeroChanges(t *testing.T) {
	ctx := context.Background()
	e := clio.NewExtractor(seeded())
	rows := extractAll(t, e)
	if len(rows) != 6 {
		t.Fatalf("extracted %d rows, want 6", len(rows))
	}
	ids := importer.NewMemIDMap()
	target := importer.Target{}
	tests := []struct {
		name        string
		ids         *importer.MemIDMap
		wantChanged int
	}{
		{"first run creates every record", ids, 6},
		{"rerun on the same snapshot", ids, 0},
		{"rerun with a fresh id map", importer.NewMemIDMap(), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmds, err := importer.TransformRows(ctx, clio.SourceID, e, rows, tc.ids)
			if err != nil {
				t.Fatalf("TransformRows: %v", err)
			}
			changed, err := target.Upsert(cmds)
			if err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			if changed != tc.wantChanged {
				t.Errorf("changed = %d, want %d", changed, tc.wantChanged)
			}
			if tc.ids.Len() != 6 {
				t.Errorf("id map entries = %d, want 6", tc.ids.Len())
			}
		})
	}
}

func TestTransformCommands(t *testing.T) {
	ctx := context.Background()
	e := clio.NewExtractor(seeded())
	rows := extractAll(t, e)
	ids := importer.NewMemIDMap()
	cmds, err := importer.TransformRows(ctx, clio.SourceID, e, rows, ids)
	if err != nil {
		t.Fatalf("TransformRows: %v", err)
	}
	byKind := map[string]importer.Command{}
	for _, c := range cmds {
		byKind[c.Kind+":"+c.TargetID] = c
	}
	matterID := importer.InternalID(clio.SourceID, "matters", "201")
	tests := []struct {
		kind, resource, ext, field, want string
	}{
		{clio.KindPartyCreate, "contacts", "101", "email", "a@x.ca"},
		{clio.KindPartyCreate, "contacts", "102", "kind", "Company"},
		{clio.KindMatterCreate, "matters", "201", "client_id", importer.InternalID(clio.SourceID, "contacts", "101")},
		{clio.KindTimeCreate, "activities", "301", "matter_id", matterID},
		{clio.KindTimeCreate, "activities", "301", "rate", "350.10"},
		{clio.KindTimeCreate, "activities", "301", "date", "2026-10-01"},
		{clio.KindBillImport, "bills", "401", "total", "1234.50"},
		{clio.KindBillImport, "bills", "401", "matter_id", matterID},
	}
	for _, tc := range tests {
		t.Run(tc.kind+"/"+tc.field, func(t *testing.T) {
			c, ok := byKind[tc.kind+":"+importer.InternalID(clio.SourceID, tc.resource, tc.ext)]
			if !ok {
				t.Fatalf("no %s command for %s:%s", tc.kind, tc.resource, tc.ext)
			}
			if got := c.Fields[tc.field]; got != tc.want {
				t.Errorf("%s = %v, want %q", tc.field, got, tc.want)
			}
		})
	}
}

func TestTransformErrors(t *testing.T) {
	ctx := context.Background()
	e := clio.NewExtractor(cliofake.New(10))
	tests := []struct {
		name string
		row  importer.Row
		want error
	}{
		{"matter with unknown client", importer.Row{Resource: "matters", ExternalID: "9",
			Fields: map[string]any{"client": map[string]any{"id": json.Number("999")}}}, importer.ErrUnresolved},
		{"non-time activity", importer.Row{Resource: "activities", ExternalID: "8",
			Fields: map[string]any{"type": "ExpenseEntry"}}, importer.ErrUnmapped},
		{"unknown resource", importer.Row{Resource: "tasks", ExternalID: "7"}, importer.ErrUnmapped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Transform(ctx, tc.row, importer.NewMemIDMap())
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
