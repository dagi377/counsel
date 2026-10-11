package importer

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// dryRunSource maps contacts (name, email) and projects; open_requests and report_presets
// have no import path; a project pointing at an unknown contact is an error.
type dryRunSource struct{}

func (dryRunSource) Resources() []string {
	return []string{"contacts", "projects", "open_requests", "report_presets"}
}

func (dryRunSource) Extract(context.Context, string, Cursor) (Page, error) {
	return Page{Done: true}, nil
}

func (dryRunSource) MappedFields(res string) []string {
	switch res {
	case "contacts":
		return []string{"name", "email"}
	case "projects":
		return []string{"owner"}
	}
	return nil
}

func (dryRunSource) Transform(ctx context.Context, r Row, m IDMap) ([]Command, error) {
	switch r.Resource {
	case "contacts":
		return []Command{{Kind: "person.create", Fields: r.Fields}}, nil
	case "projects":
		owner, _ := r.Fields["owner"].(string)
		if _, err := Resolve(ctx, m, "ext", "contacts", owner); err != nil {
			return nil, err
		}
		return []Command{{Kind: "project.create"}}, nil
	}
	return nil, &UnmappedError{Resource: r.Resource}
}

type stubGate struct {
	err   error
	calls int
}

func (g *stubGate) Approve(context.Context, DryRun) error { g.calls++; return g.err }

func TestBuildDryRun(t *testing.T) {
	ctx := context.Background()
	contact := func(id string, f map[string]any) Row { return Row{Resource: "contacts", ExternalID: id, Fields: f} }
	tests := []struct {
		name         string
		rows         []Row
		wantCounts   map[string]int
		wantUnmapped map[string]int
		wantErrors   int
	}{
		{
			// rule: MIG-012
			name: "not-moved resources listed with counts",
			rows: []Row{
				{Resource: "open_requests", ExternalID: "1"}, {Resource: "open_requests", ExternalID: "2"},
				{Resource: "open_requests", ExternalID: "3"},
				{Resource: "report_presets", ExternalID: "1"}, {Resource: "report_presets", ExternalID: "2"},
			},
			wantCounts:   map[string]int{"open_requests": 3, "report_presets": 2},
			wantUnmapped: map[string]int{"open_requests": 3, "report_presets": 2},
		},
		{
			// rule: MIG-013
			name:         "unknown field surfaced with count",
			rows:         []Row{contact("1", map[string]any{"name": "A", "nickname": "Al"}), contact("2", map[string]any{"name": "B"})},
			wantCounts:   map[string]int{"contacts": 2},
			wantUnmapped: map[string]int{"contacts.nickname": 1},
		},
		{
			name:         "unresolved reference is an error",
			rows:         []Row{contact("1", nil), {Resource: "projects", ExternalID: "9", Fields: map[string]any{"owner": "999"}}},
			wantCounts:   map[string]int{"contacts": 1, "projects": 1},
			wantUnmapped: map[string]int{},
			wantErrors:   1,
		},
		{
			name:         "clean snapshot",
			rows:         []Row{contact("1", nil), {Resource: "projects", ExternalID: "9", Fields: map[string]any{"owner": "1"}}},
			wantCounts:   map[string]int{"contacts": 1, "projects": 1},
			wantUnmapped: map[string]int{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := BuildDryRun(ctx, "ext", "snap-1", dryRunSource{}, tc.rows)
			if err != nil {
				t.Fatalf("BuildDryRun: %v", err)
			}
			if !reflect.DeepEqual(d.Counts, tc.wantCounts) {
				t.Errorf("counts = %v, want %v", d.Counts, tc.wantCounts)
			}
			if !reflect.DeepEqual(d.Unmapped, tc.wantUnmapped) {
				t.Errorf("unmapped = %v, want %v", d.Unmapped, tc.wantUnmapped)
			}
			if len(d.Errors) != tc.wantErrors {
				t.Errorf("errors = %v, want %d", d.Errors, tc.wantErrors)
			}
		})
	}
}

func TestUnmappedErrorMatchesSentinel(t *testing.T) {
	for _, e := range []*UnmappedError{{Resource: "contacts"}, {Resource: "contacts", Field: "nickname"}} {
		if !errors.Is(e, ErrUnmapped) {
			t.Errorf("%v does not match ErrUnmapped", e)
		}
	}
}

func TestSnapshotSHA(t *testing.T) {
	a := SnapshotSHA([]string{"p1", "p2"})
	tests := []struct {
		name string
		shas []string
		same bool
	}{
		{"order is irrelevant", []string{"p2", "p1"}, true},
		// rule: MIG-015
		{"new page changes the snapshot", []string{"p1", "p2", "p3"}, false},
		{"changed page changes the snapshot", []string{"p1", "p2x"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SnapshotSHA(tc.shas) == a; got != tc.same {
				t.Errorf("same = %v, want %v", got, tc.same)
			}
		})
	}
}

// rule: MIG-015
func TestImportGate(t *testing.T) {
	ctx := context.Background()
	clean := DryRun{Source: "ext", Snapshot: "snap-1"}
	dirty := DryRun{Source: "ext", Snapshot: "snap-1", Errors: []Issue{{Message: "unresolved"}}}
	denied := errors.New("owner rejected")
	tests := []struct {
		name        string
		dry         DryRun
		gateErr     error
		approve     bool
		runSnapshot string
		wantApprove error
		wantRun     error
		wantCalls   int
	}{
		{name: "no dry run", runSnapshot: "snap-1", wantRun: ErrNoApprovedDryRun},
		{name: "dry run not approved", dry: clean, runSnapshot: "snap-1", wantRun: ErrNoApprovedDryRun},
		{name: "dry run with errors cannot be approved", dry: dirty, approve: true, runSnapshot: "snap-1",
			wantApprove: ErrDryRunHasErrors, wantRun: ErrNoApprovedDryRun},
		{name: "gate rejects", dry: clean, gateErr: denied, approve: true, runSnapshot: "snap-1",
			wantApprove: denied, wantRun: ErrNoApprovedDryRun, wantCalls: 1},
		{name: "approved on another snapshot", dry: clean, approve: true, runSnapshot: "snap-2",
			wantRun: ErrNoApprovedDryRun, wantCalls: 1},
		{name: "approved zero-error dry run on same snapshot", dry: clean, approve: true, runSnapshot: "snap-1", wantCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := &stubGate{err: tc.gateErr}
			r := NewDryRuns(g)
			if tc.dry.Source != "" {
				d := r.Record(tc.dry)
				if tc.approve {
					err := r.Approve(ctx, d.ID)
					if (err == nil) != (tc.wantApprove == nil) || (tc.wantApprove != nil && !errors.Is(err, tc.wantApprove)) {
						t.Fatalf("Approve = %v, want %v", err, tc.wantApprove)
					}
				}
			}
			if g.calls != tc.wantCalls {
				t.Errorf("gate calls = %d, want %d", g.calls, tc.wantCalls)
			}
			err := r.RequireApproved("ext", tc.runSnapshot)
			if (err == nil) != (tc.wantRun == nil) || (tc.wantRun != nil && !errors.Is(err, tc.wantRun)) {
				t.Errorf("RequireApproved = %v, want %v", err, tc.wantRun)
			}
		})
	}
}

func TestApproveUnknownAndNoGate(t *testing.T) {
	ctx := context.Background()
	if err := NewDryRuns(&stubGate{}).Approve(ctx, "nope"); !errors.Is(err, ErrNoApprovedDryRun) {
		t.Errorf("unknown id: %v", err)
	}
	r := NewDryRuns(nil)
	d := r.Record(DryRun{Source: "ext", Snapshot: "s"})
	if err := r.Approve(ctx, d.ID); !errors.Is(err, ErrNoApprovedDryRun) {
		t.Errorf("no gate: %v", err)
	}
}
