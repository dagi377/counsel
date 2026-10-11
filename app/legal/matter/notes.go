package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/audit"
)

var (
	// ErrEmptyNote is returned for a note with no text.
	ErrEmptyNote = errors.New("matter: note has no text")
	// ErrStaleVersion is returned when a revision is based on a version that is not the latest.
	ErrStaleVersion = errors.New("matter: version is not the latest")
	// ErrInvalidFact is returned for a fact with no key or a value that is not JSON.
	ErrInvalidFact = errors.New("matter: invalid fact")
)

// Bodies stores note text as an encrypted object and returns its reference (DM-020). Note
// text never reaches the database row, logs, errors or audit detail; only the reference does.
type Bodies interface {
	Put(ctx context.Context, text string) (ref string, err error)
}

// Note is one version of a matter.note. A revision appends a new version with the same ID.
type Note struct {
	ID        string
	MatterID  ID
	AuthorID  string
	BodyRef   string
	Version   int
	CreatedAt time.Time
}

// Fact is one version of a matter.fact. A new value for a key supersedes the current fact.
type Fact struct {
	ID           string
	MatterID     ID
	Key          string
	Value        json.RawMessage
	SourceRefs   []string
	CreatedBy    string
	CreatedAt    time.Time
	SupersededBy string // empty while current
}

// Notes keeps matter notes and facts. Both are append-only: a revision adds a new version and
// nothing is updated in place, except that a superseded fact records what superseded it.
type Notes struct {
	bodies Bodies
	audit  audit.Log
	now    func() time.Time

	mu    sync.Mutex
	notes []Note
	facts []Fact
}

// NewNotes returns Notes that stores note text in b, records audit events in a and stamps
// times from now.
func NewNotes(b Bodies, a audit.Log, now func() time.Time) *Notes {
	return &Notes{bodies: b, audit: a, now: now}
}

// AddNote stores text and appends version 1 of a new note.
func (n *Notes) AddNote(ctx context.Context, matterID ID, authorID, text string) (Note, error) {
	return n.write(ctx, matterID, authorID, "", 0, text)
}

// ReviseNote appends a new version of note id. base is the version the author edited; it must
// be the latest, so a concurrent revision is never silently lost.
func (n *Notes) ReviseNote(ctx context.Context, id string, base int, authorID, text string) (Note, error) {
	n.mu.Lock()
	latest, ok := n.latestNote(id)
	n.mu.Unlock()
	if !ok {
		return Note{}, fmt.Errorf("revise note: %w", ErrNotFound)
	}
	if latest.Version != base {
		return Note{}, fmt.Errorf("revise note: %w", ErrStaleVersion)
	}
	return n.write(ctx, latest.MatterID, authorID, id, base, text)
}

func (n *Notes) write(ctx context.Context, matterID ID, authorID, id string, base int, text string) (Note, error) {
	if matterID == "" || authorID == "" {
		return Note{}, fmt.Errorf("write note: missing field: %w", ErrInvalid)
	}
	if strings.TrimSpace(text) == "" {
		return Note{}, ErrEmptyNote
	}
	ref, err := n.bodies.Put(ctx, text)
	if err != nil {
		// The text is client data: the error never carries it.
		return Note{}, fmt.Errorf("write note: store body: %w", err)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if id == "" {
		id = "note-" + strconv.Itoa(len(n.notes)+1)
	} else if latest, _ := n.latestNote(id); latest.Version != base {
		return Note{}, fmt.Errorf("revise note: %w", ErrStaleVersion)
	}
	note := Note{ID: id, MatterID: matterID, AuthorID: authorID, BodyRef: ref, Version: base + 1, CreatedAt: n.now()}
	if err := n.record(ctx, authorID, "matter.note.write", id, matterID, note.Version); err != nil {
		return Note{}, err
	}
	n.notes = append(n.notes, note)
	slog.InfoContext(ctx, "matter note written", "matter_id", string(matterID), "note_id", id, "version", note.Version)
	return note, nil
}

// NoteVersions lists every version of note id, oldest first.
func (n *Notes) NoteVersions(_ context.Context, id string) ([]Note, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []Note
	for _, v := range n.notes {
		if v.ID == id {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func (n *Notes) latestNote(id string) (Note, bool) {
	for i := len(n.notes) - 1; i >= 0; i-- {
		if n.notes[i].ID == id {
			return n.notes[i], true
		}
	}
	return Note{}, false
}

// SetFact appends a fact for key on the matter and supersedes the current fact for that key.
func (n *Notes) SetFact(ctx context.Context, matterID ID, key string, value json.RawMessage, sources []string, by string) (Fact, error) {
	key = strings.TrimSpace(key)
	if matterID == "" || key == "" || by == "" || !json.Valid(value) {
		return Fact{}, fmt.Errorf("set fact: %w", ErrInvalidFact)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	f := Fact{
		ID:         "fact-" + strconv.Itoa(len(n.facts)+1),
		MatterID:   matterID,
		Key:        key,
		Value:      append(json.RawMessage(nil), value...),
		SourceRefs: append([]string(nil), sources...),
		CreatedBy:  by,
		CreatedAt:  n.now(),
	}
	if err := n.record(ctx, by, "matter.fact.set", f.ID, matterID, 0); err != nil {
		return Fact{}, err
	}
	for i := range n.facts {
		if n.facts[i].MatterID == matterID && n.facts[i].Key == key && n.facts[i].SupersededBy == "" {
			n.facts[i].SupersededBy = f.ID
		}
	}
	n.facts = append(n.facts, f)
	return f, nil
}

// Facts lists the matter's facts, every version, oldest first. Current facts have no
// SupersededBy.
func (n *Notes) Facts(_ context.Context, matterID ID) ([]Fact, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []Fact
	for _, f := range n.facts {
		if f.MatterID == matterID {
			out = append(out, f)
		}
	}
	return out, nil
}

// record appends an audit event that names ids and versions only, never note text or fact values.
func (n *Notes) record(ctx context.Context, actor, action, subjectID string, matterID ID, version int) error {
	detail := map[string]string{"matter_id": string(matterID)}
	if version > 0 {
		detail["version"] = strconv.Itoa(version)
	}
	e := audit.Event{
		ActorKind: "user",
		ActorID:   actor,
		Action:    action,
		Subject:   audit.Subject{Kind: strings.Split(action, ".")[1], ID: subjectID},
		Detail:    detail,
	}
	if err := n.audit.Append(ctx, nil, e); err != nil {
		return fmt.Errorf("%s: audit: %w", action, err)
	}
	return nil
}
