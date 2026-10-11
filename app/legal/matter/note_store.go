package matter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/outbox"
)

// EventNoteAdded is published when a person adds a note to a matter
// (spec/eng/a2-interfaces.json, Q-EVENTS-WIRE-4).
const EventNoteAdded = "matter.note_added"

// NewNote is a note a person adds to a matter. BodyRef is the object key of the encrypted
// body (DM-020, Bodies.Put); the database and the event never hold the text.
type NewNote struct {
	MatterID   ID
	AuthorID   string
	BodyRef    string
	Visibility string // private, team (default), firm or client
	Privileged bool
}

// NoteAdded is the body of EventNoteAdded.
type NoteAdded struct {
	MatterID   string `json:"matter_id"`
	NoteID     string `json:"note_id"`
	AuthorID   string `json:"author_id"`
	Visibility string `json:"visibility"`
}

const addNoteSQL = `INSERT INTO matter.note (matter_id, author_id, body_ref, visibility, privileged, updated_by)
SELECT id, $2::uuid, $3, $4, $5, $2::uuid FROM matter.matter WHERE id = $1::uuid AND archived_at IS NULL
RETURNING id::text`

// AddNote stores n in matter.note and publishes EventNoteAdded in the same tx. It returns the
// note id. A matter that does not exist or is archived is ErrNotFound.
func (s *Store) AddNote(ctx context.Context, n NewNote) (string, error) {
	if n.Visibility == "" {
		n.Visibility = "team"
	}
	switch n.Visibility {
	case "private", "team", "firm":
	case "client":
		if n.Privileged {
			return "", fmt.Errorf("add note: privileged client note: %w", ErrInvalid)
		}
	default:
		return "", fmt.Errorf("add note: visibility %q: %w", n.Visibility, ErrInvalid)
	}
	if strings.TrimSpace(string(n.MatterID)) == "" || n.AuthorID == "" || n.BodyRef == "" {
		return "", fmt.Errorf("add note: %w", ErrInvalid)
	}
	if s.Events == nil {
		return "", fmt.Errorf("add note: no event emitter: %w", ErrInvalid)
	}
	var id string
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, addNoteSQL, string(n.MatterID), n.AuthorID, n.BodyRef, n.Visibility, n.Privileged).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// rule: ARC-I-030
		body := NoteAdded{MatterID: string(n.MatterID), NoteID: id, AuthorID: n.AuthorID, Visibility: n.Visibility}
		return s.Events.Emit(ctx, tx, outbox.Subject{Kind: matterkind.Subject, ID: string(n.MatterID)}, EventNoteAdded, body)
	})
	if err != nil {
		return "", fmt.Errorf("add note %s: %w", n.MatterID, err)
	}
	return id, nil
}
