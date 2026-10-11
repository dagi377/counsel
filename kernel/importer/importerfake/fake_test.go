package importerfake

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/dagi377/counsel/kernel/importer"
)

// pagedSource serves rows "r0".."rN-1" of one resource, two per page. Rows whose index is
// in bad fail to transform as unmapped. failAt makes Extract fail once at that cursor.
type pagedSource struct {
	n      int
	bad    map[int]bool
	failAt importer.Cursor
}

func (s *pagedSource) Resources() []string { return []string{"people"} }

func (s *pagedSource) Extract(_ context.Context, _ string, cur importer.Cursor) (importer.Page, error) {
	if s.failAt != "" && cur == s.failAt {
		s.failAt = ""
		return importer.Page{}, errors.New("killed")
	}
	start := 0
	if cur != "" {
		start, _ = strconv.Atoi(string(cur))
	}
	var p importer.Page
	for i := start; i < start+2 && i < s.n; i++ {
		p.Rows = append(p.Rows, importer.Row{Resource: "people", ExternalID: "r" + strconv.Itoa(i)})
	}
	next := start + 2
	p.Next = importer.Cursor(strconv.Itoa(next))
	p.Done = next >= s.n
	return p, nil
}

func (s *pagedSource) Transform(ctx context.Context, r importer.Row, m importer.IDMap) ([]importer.Command, error) {
	i, _ := strconv.Atoi(r.ExternalID[1:])
	if s.bad[i] {
		return nil, fmt.Errorf("field x: %w", importer.ErrUnmapped)
	}
	id, err := m.Assign(ctx, "src", r.Resource, r.ExternalID)
	if err != nil {
		return nil, err
	}
	return []importer.Command{{Kind: "people.create", TargetID: id}}, nil
}

func TestDryRunAndGate(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		bad          map[int]bool
		approve      bool
		runSnapshot  string
		wantCount    int
		wantUnmapped int
		approveErr   error
		runErr       error
	}{
		{name: "clean dry run approved", approve: true, runSnapshot: "s1", wantCount: 5},
		{name: "run without approval", runSnapshot: "s1", wantCount: 5, runErr: importer.ErrNoApprovedDryRun},
		{name: "approval on another snapshot", approve: true, runSnapshot: "s2", wantCount: 5, runErr: importer.ErrNoApprovedDryRun},
		{name: "dry run with errors cannot be approved", bad: map[int]bool{3: true}, approve: true, runSnapshot: "s1",
			wantCount: 5, wantUnmapped: 1, approveErr: importer.ErrDryRunHasErrors, runErr: importer.ErrNoApprovedDryRun},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if err := f.Register("src", &pagedSource{n: 5, bad: tt.bad}); err != nil {
				t.Fatal(err)
			}
			d, err := f.DryRun(ctx, "src", "s1")
			if err != nil {
				t.Fatal(err)
			}
			if d.Counts["people"] != tt.wantCount || d.Unmapped["people"] != tt.wantUnmapped {
				t.Fatalf("counts=%v unmapped=%v", d.Counts, d.Unmapped)
			}
			if f.IDs.Len() != 0 {
				t.Fatalf("dry run wrote %d id map rows", f.IDs.Len())
			}
			if tt.approve {
				if err := f.Approve(ctx, d.ID); !errors.Is(err, tt.approveErr) {
					t.Fatalf("Approve err = %v, want %v", err, tt.approveErr)
				}
			}
			if err := f.Run(ctx, "src", tt.runSnapshot); !errors.Is(err, tt.runErr) {
				t.Fatalf("Run err = %v, want %v", err, tt.runErr)
			}
		})
	}
}

func TestRunResumesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		failAt  importer.Cursor
		wantErr bool
	}{
		{name: "no kill"},
		{name: "killed mid-extract resumes from checkpoint", failAt: "2", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &pagedSource{n: 5}
			f := New()
			if err := f.Register("src", src); err != nil {
				t.Fatal(err)
			}
			d, err := f.DryRun(ctx, "src", "s1")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Approve(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			src.failAt = tt.failAt
			err = f.Run(ctx, "src", "s1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("first Run err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				cur, _ := f.Checkpoint(ctx, "src", "people")
				if cur != tt.failAt {
					t.Fatalf("checkpoint = %q, want %q", cur, tt.failAt)
				}
				if err := f.Run(ctx, "src", "s1"); err != nil {
					t.Fatalf("resume: %v", err)
				}
			}
			if len(f.Applied) != 5 || f.IDs.Len() != 5 {
				t.Fatalf("applied=%d ids=%d, want 5 and 5", len(f.Applied), f.IDs.Len())
			}
			// A rerun after completion changes nothing.
			if err := f.Run(ctx, "src", "s1"); err != nil {
				t.Fatal(err)
			}
			if len(f.Applied) != 5 || f.IDs.Len() != 5 {
				t.Fatalf("rerun changed state: applied=%d ids=%d", len(f.Applied), f.IDs.Len())
			}
		})
	}
}

func TestRegisterAndUnknown(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		run  func(f *Fake) error
		err  error
	}{
		{name: "duplicate source", run: func(f *Fake) error {
			_ = f.Register("a", &pagedSource{})
			return f.Register("a", &pagedSource{})
		}, err: importer.ErrDuplicateSource},
		{name: "dry run unknown source", run: func(f *Fake) error {
			_, err := f.DryRun(ctx, "z", "s")
			return err
		}, err: importer.ErrUnknownSource},
		{name: "approve unknown dry run", run: func(f *Fake) error { return f.Approve(ctx, "dry-404") }, err: importer.ErrNoApprovedDryRun},
		{name: "checkpoint unknown source", run: func(f *Fake) error {
			_, err := f.Checkpoint(ctx, "z", "r")
			return err
		}, err: importer.ErrUnknownSource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(New()); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}
