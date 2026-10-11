// CONTRACT: kernel/ids types and interfaces. Frozen once on main; changes are additive only
// and need a docs/QUESTIONS.md entry. Source: spec/eng/k1-kernel.html.

package ids

import "errors"

// ID is a UUIDv7 in canonical lower-case text form (8-4-4-4-12). IDs from one process
// increase strictly; IDs from SQL core.uuidv7() sort by millisecond only.
type ID string

// Generator issues new IDs.
type Generator interface {
	// New returns an ID greater than every ID this Generator issued before.
	New() ID
}

// ErrInvalid is returned when text is not a canonical UUID.
var ErrInvalid = errors.New("ids: invalid id")
