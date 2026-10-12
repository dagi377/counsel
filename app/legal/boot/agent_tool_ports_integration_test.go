//go:build integration

package boot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/clock"
	kdocs "github.com/dagi377/counsel/kernel/docs"
)

// TestAgentToolPortsPostgres reads through the six Postgres tool ports on an empty firm:
// unknown versions, entries and drafts fail closed, a matter has no filed documents, and a
// batch draft outside an agent run has no preparer.
func TestAgentToolPortsPostgres(t *testing.T) {
	pool := billingPortsDB(t)
	ctx := context.Background()
	const none = "00000000-0000-0000-0000-000000000001"
	folders, err := legaldocs.NewFolderStore(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	drafts := draft.PGDrafts{}
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"text of an unknown version", func() error {
			_, err := kdocs.PGText{DB: pool}.Lines(ctx, kdocs.VersionID(none), 1)
			return err
		}, kdocs.ErrNotFound},
		{"no filed documents", func() error {
			got, err := filedDocuments{s: folders}.MatterDocuments(ctx, none)
			if err == nil && len(got) != 0 {
				return errors.New("documents on an empty matter")
			}
			return err
		}, nil},
		{"unknown entry", func() error { _, err := tk.PGEntries{DB: pool}.Get(ctx, none); return err }, tk.ErrNotFound},
		{"no entries", func() error {
			got, err := tk.PGEntries{DB: pool}.List(ctx, tk.EntryFilter{Limit: 10})
			if err == nil && len(got) != 0 {
				return errors.New("entries on an empty firm")
			}
			return err
		}, nil},
		{"lines of an unknown draft", func() error {
			_, err := draft.PGStore{DB: pool, Drafts: drafts}.Lines(ctx, none)
			return err
		}, draft.ErrNotFound},
		{"save lines of an unknown draft", func() error {
			return draft.PGStore{DB: pool, Drafts: drafts}.SaveLines(ctx, none, nil)
		}, draft.ErrNotFound},
		{"batch draft outside a run", func() error {
			b := draft.PGBatch{DB: pool, Drafts: drafts, PreparedBy: runActingFor(pool)}
			_, _, err := b.Create(ctx, "run|matter:"+none+"|2026-09-30", batch.Target{MatterID: none},
				clock.Date{Year: 2026, Month: time.September, Day: 30})
			return err
		}, agent.ErrMissingDep},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
