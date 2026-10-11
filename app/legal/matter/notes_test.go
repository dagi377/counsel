package matter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
)

var notesNow = time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)

// memBodies stands in for the encrypted object store.
type memBodies struct{ texts []string }

func (b *memBodies) Put(_ context.Context, text string) (string, error) {
	b.texts = append(b.texts, text)
	return fmt.Sprintf("obj-%d", len(b.texts)), nil
}

// newNotes returns Notes whose audit fake keeps every detail value unmasked, so a leak shows.
func newNotes() (*matter.Notes, *memBodies, *auditfake.Fake) {
	b := &memBodies{}
	a := auditfake.New("matter_id", "version", "text", "body", "value")
	return matter.NewNotes(b, a, func() time.Time { return notesNow }), b, a
}

func TestNotesAreAppendOnlyVersions(t *testing.T) {
	tests := []struct {
		name     string
		revise   []int // base version of each revision after AddNote
		wantErr  error
		wantVers int
	}{
		{"add only", nil, nil, 1},
		{"two revisions", []int{1, 2}, nil, 3},
		{"stale base", []int{1, 1}, matter.ErrStaleVersion, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			n, _, _ := newNotes()
			note, err := n.AddNote(ctx, "m1", "u-law", "first draft")
			if err != nil {
				t.Fatal(err)
			}
			var lastErr error
			for i, base := range tc.revise {
				if _, err := n.ReviseNote(ctx, note.ID, base, "u-law", fmt.Sprintf("draft %d", i+2)); err != nil {
					lastErr = err
				}
			}
			if !errors.Is(lastErr, tc.wantErr) {
				t.Fatalf("err = %v, want %v", lastErr, tc.wantErr)
			}
			vers, err := n.NoteVersions(ctx, note.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(vers) != tc.wantVers {
				t.Fatalf("versions = %d, want %d", len(vers), tc.wantVers)
			}
			for i, v := range vers {
				if v.Version != i+1 || v.BodyRef == "" {
					t.Fatalf("version %d = %+v", i, v)
				}
			}
			if vers[0].BodyRef == vers[len(vers)-1].BodyRef && len(vers) > 1 {
				t.Fatal("revision overwrote the first version's body")
			}
		})
	}
}

func TestNoteValidation(t *testing.T) {
	tests := []struct {
		name           string
		matter, author string
		text           string
		wantErr        error
	}{
		{"empty text", "m1", "u-law", "  ", matter.ErrEmptyNote},
		{"no author", "m1", "", "text", matter.ErrInvalid},
		{"no matter", "", "u-law", "text", matter.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, _, _ := newNotes()
			if _, err := n.AddNote(context.Background(), matter.ID(tc.matter), tc.author, tc.text); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
	n, _, _ := newNotes()
	if _, err := n.ReviseNote(context.Background(), "note-9", 1, "u-law", "x"); !errors.Is(err, matter.ErrNotFound) {
		t.Fatalf("revise unknown: err = %v", err)
	}
}

func TestNoteTextNeverInLogsOrAudit(t *testing.T) {
	const secret = "client admitted the contract was backdated"
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	n, bodies, a := newNotes()
	note, err := n.AddNote(ctx, "m1", "u-law", secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.ReviseNote(ctx, note.ID, 1, "u-law", secret+" (revised)"); err != nil {
		t.Fatal(err)
	}
	if _, err := n.SetFact(ctx, "m1", "admission", json.RawMessage(`"`+secret+`"`), nil, "u-law"); err != nil {
		t.Fatal(err)
	}
	if len(bodies.texts) != 2 || bodies.texts[0] != secret {
		t.Fatalf("body store = %q", bodies.texts)
	}
	events := a.Events()
	if len(events) != 3 {
		t.Fatalf("audit events = %d, want 3", len(events))
	}
	for _, e := range events {
		for k, v := range e.Detail {
			if strings.Contains(v, "backdated") {
				t.Fatalf("audit detail %s leaks note text: %q", k, v)
			}
		}
	}
	if strings.Contains(logs.String(), "backdated") {
		t.Fatalf("logs leak note text: %s", logs.String())
	}
	if !strings.Contains(logs.String(), note.ID) {
		t.Fatalf("logs do not name the note id: %s", logs.String())
	}
}

func TestFactsSupersedeNeverEdit(t *testing.T) {
	ctx := context.Background()
	n, _, _ := newNotes()
	tests := []struct {
		name    string
		matter  string
		key     string
		value   string
		wantErr error
	}{
		{"first value", "m1", "incident_date", `"2025-11-03"`, nil},
		{"other key", "m1", "court_file", `"CV-25-1"`, nil},
		{"new value supersedes", "m1", "incident_date", `"2025-11-04"`, nil},
		{"other matter same key", "m2", "incident_date", `"2024-01-01"`, nil},
		{"bad json", "m1", "incident_date", `{`, matter.ErrInvalidFact},
		{"no key", "m1", " ", `1`, matter.ErrInvalidFact},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := n.SetFact(ctx, matter.ID(tc.matter), tc.key, json.RawMessage(tc.value), []string{"doc-1"}, "u-law")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
	facts, err := n.Facts(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 3 {
		t.Fatalf("m1 facts = %d, want 3", len(facts))
	}
	first, third := facts[0], facts[2]
	if string(first.Value) != `"2025-11-03"` || first.SupersededBy != third.ID {
		t.Fatalf("first version = %+v; want unchanged value, superseded by %s", first, third.ID)
	}
	if facts[1].SupersededBy != "" || third.SupersededBy != "" {
		t.Fatalf("current facts superseded: %+v", facts)
	}
	m2, _ := n.Facts(ctx, "m2")
	if len(m2) != 1 || m2[0].SupersededBy != "" {
		t.Fatalf("m2 facts = %+v", m2)
	}
}
