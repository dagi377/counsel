package importer

import (
	"context"
	"errors"
	"testing"
)

// refSource maps "contact" rows to contact.create and "entry" rows to entry.create; an entry
// references a contact through its "contact" field.
type refSource struct{}

func (refSource) Resources() []string { return []string{"contact", "entry"} }

func (refSource) Extract(context.Context, string, Cursor) (Page, error) { return Page{Done: true}, nil }

func (refSource) Transform(ctx context.Context, r Row, m IDMap) ([]Command, error) {
	id, err := Resolve(ctx, m, "src", r.Resource, r.ExternalID)
	if err != nil {
		return nil, err
	}
	switch r.Resource {
	case "contact":
		return []Command{{Kind: "contact.create", TargetID: id, Fields: map[string]any{"email": r.Fields["email"]}}}, nil
	case "entry":
		ref, err := Resolve(ctx, m, "src", "contact", r.Fields["contact"].(string))
		if err != nil {
			return nil, err
		}
		return []Command{{Kind: "entry.create", TargetID: id, Fields: map[string]any{"contact_id": ref}}}, nil
	}
	return nil, ErrUnmapped
}

func fixtureRows() []Row {
	return []Row{
		{Resource: "entry", ExternalID: "e1", Fields: map[string]any{"contact": "c2"}},
		{Resource: "contact", ExternalID: "c1", Fields: map[string]any{"email": "a@x.ca"}},
		{Resource: "contact", ExternalID: "c2", Fields: map[string]any{"email": "A@x.ca "}},
		{Resource: "contact", ExternalID: "c3", Fields: map[string]any{"email": "b@x.ca"}},
	}
}

func TestMIG_007_OneIDMapRowPerImportedRow(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		runs  int
		wantN int
	}{
		{"single run", 1, 4},
		{"rerun adds no id map rows", 3, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMemIDMap()
			for i := 0; i < tc.runs; i++ {
				if _, err := TransformRows(ctx, "src", refSource{}, fixtureRows(), m); err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
			}
			if got := m.Len(); got != tc.wantN {
				t.Fatalf("id map rows = %d, want %d", got, tc.wantN)
			}
			for _, r := range fixtureRows() {
				id, ok, _ := m.Lookup(ctx, "src", r.Resource, r.ExternalID)
				if !ok || id != InternalID("src", r.Resource, r.ExternalID) {
					t.Fatalf("%s:%s -> %q, %v", r.Resource, r.ExternalID, id, ok)
				}
			}
		})
	}
}

func TestMIG_007_UnresolvedReferenceIsAnError(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		rows    []Row
		wantErr error
	}{
		{"resolved", fixtureRows(), nil},
		{"missing contact", []Row{{Resource: "entry", ExternalID: "e9", Fields: map[string]any{"contact": "c999"}}}, ErrUnresolved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TransformRows(ctx, "src", refSource{}, tc.rows, NewMemIDMap())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestMIG_008_RerunTransformChangesNothing(t *testing.T) {
	ctx := context.Background()
	reversed := fixtureRows()
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	tests := []struct {
		name   string
		second []Row
		fresh  bool // second run uses a fresh id map (same snapshot, new process)
		want   int
	}{
		{"same snapshot", fixtureRows(), false, 0},
		{"same snapshot, other staging order", reversed, false, 0},
		{"same snapshot, fresh id map", fixtureRows(), true, 0},
		{"changed field", []Row{{Resource: "contact", ExternalID: "c3", Fields: map[string]any{"email": "z@x.ca"}}}, false, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMemIDMap()
			target := Target{}
			cmds, err := TransformRows(ctx, "src", refSource{}, fixtureRows(), m)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := target.Upsert(cmds); err != nil || n != 4 {
				t.Fatalf("first run changed %d, err %v", n, err)
			}
			if tc.fresh {
				m = NewMemIDMap()
			}
			cmds, err = TransformRows(ctx, "src", refSource{}, tc.second, m)
			if err != nil {
				t.Fatal(err)
			}
			n, err := target.Upsert(cmds)
			if err != nil || n != tc.want {
				t.Fatalf("second run changed %d, err %v; want %d", n, err, tc.want)
			}
		})
	}
}

func TestMIG_014_DuplicatesFlaggedNeverMerged(t *testing.T) {
	ctx := context.Background()
	email := func(r Row) string { s, _ := r.Fields["email"].(string); return s }
	tests := []struct {
		name string
		rows []Row
		want []DuplicateCandidate
	}{
		{"shared email", fixtureRows(), []DuplicateCandidate{{Resource: "contact", Key: "a@x.ca", A: "c1", B: "c2"}}},
		{"no key", []Row{{Resource: "contact", ExternalID: "c1"}, {Resource: "contact", ExternalID: "c2"}}, nil},
		{"three way", []Row{
			{Resource: "contact", ExternalID: "c1", Fields: map[string]any{"email": "a@x.ca"}},
			{Resource: "contact", ExternalID: "c2", Fields: map[string]any{"email": "a@x.ca"}},
			{Resource: "contact", ExternalID: "c3", Fields: map[string]any{"email": "a@x.ca"}},
		}, []DuplicateCandidate{
			{Resource: "contact", Key: "a@x.ca", A: "c1", B: "c2"},
			{Resource: "contact", Key: "a@x.ca", A: "c1", B: "c3"},
			{Resource: "contact", Key: "a@x.ca", A: "c2", B: "c3"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FlagDuplicates(tc.rows, email)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
			// Both rows of every pair are still imported: no merge.
			cmds, err := TransformRows(ctx, "src", refSource{}, contactsOnly(tc.rows), NewMemIDMap())
			if err != nil {
				t.Fatal(err)
			}
			if want := len(contactsOnly(tc.rows)); len(cmds) != want {
				t.Fatalf("imported %d contacts, want %d", len(cmds), want)
			}
		})
	}
}

func contactsOnly(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if r.Resource == "contact" {
			out = append(out, r)
		}
	}
	return out
}
