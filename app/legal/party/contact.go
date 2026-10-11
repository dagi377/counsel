package party

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// pgInvalidText is the Postgres SQLSTATE for a malformed value, such as an id that is not a uuid.
const pgInvalidText = "22P02"

// pgUniqueViolation is the Postgres SQLSTATE for a unique constraint violation.
const pgUniqueViolation = "23505"

// ErrDuplicate means the contact already exists (a unique key on party.contact). The HTTP
// layer answers 409 duplicate_contact.
var ErrDuplicate = errors.New("party: duplicate contact")

// ListLimit caps one List page: one more than the largest API page (200), so a caller can
// tell whether another page follows.
const ListLimit = 201

// Filter selects one page of List: contacts whose id sorts after After, at most Limit
// (0 means ListLimit; larger values are capped).
type Filter struct {
	After ContactID
	Limit int
}

// PageSize is f.Limit with the default and cap applied.
func (f Filter) PageSize() int {
	if f.Limit <= 0 || f.Limit > ListLimit {
		return ListLimit
	}
	return f.Limit
}

// Store creates and reads party.contact rows. It has no conflict check; that is the conflict
// module's job.
type Store struct {
	db db.DB
	// Events receives EventOrganizationClientCreated in Create's tx. Boot sets it; nil
	// publishes nothing.
	Events outbox.Emitter
}

var _ Contacts = (*Store)(nil)

// NewStore returns a Store on pool.
func NewStore(pool db.DB) *Store { return &Store{db: pool} }

// validateContact checks c and returns its preferred language with the default applied.
// Errors name the field, never its value: names are client data.
func validateContact(c NewContact) (string, error) {
	if c.Kind != Person && c.Kind != Organization {
		return "", fmt.Errorf("kind: %w", ErrInvalid)
	}
	if c.LegalName == "" || c.DisplayName == "" {
		return "", fmt.Errorf("name required: %w", ErrInvalid)
	}
	lang := c.PreferredLanguage
	if lang == "" {
		lang = "en"
	}
	if lang != "en" && lang != "fr" {
		return "", fmt.Errorf("language: %w", ErrInvalid)
	}
	return lang, nil
}

const insertContactSQL = `INSERT INTO party.contact
  (kind, legal_name, display_name, preferred_language, is_client, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($6, '')::uuid)
RETURNING id::text, state, created_at, row_version`

// Create implements Contacts. A new contact is proposed.
func (s *Store) Create(ctx context.Context, c NewContact) (Contact, error) {
	lang, err := validateContact(c)
	if err != nil {
		return Contact{}, fmt.Errorf("create contact: %w", err)
	}
	out := Contact{
		Kind:              c.Kind,
		LegalName:         c.LegalName,
		DisplayName:       c.DisplayName,
		PreferredLanguage: lang,
		IsClient:          c.IsClient,
	}
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id, state string
		if err := tx.QueryRow(ctx, insertContactSQL,
			string(c.Kind), c.LegalName, c.DisplayName, lang, c.IsClient, c.CreatedBy,
		).Scan(&id, &state, &out.CreatedAt, &out.RowVersion); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == pgUniqueViolation {
				return ErrDuplicate
			}
			return fmt.Errorf("insert: %w", err)
		}
		out.ID = ContactID(id)
		out.State = State(state)
		// The event commits with the contact row.
		return s.publishCreated(ctx, tx, out, c.CreatedBy)
	})
	if err != nil {
		return Contact{}, fmt.Errorf("create contact: %w", err)
	}
	return out, nil
}

const getContactSQL = `SELECT id::text, kind, legal_name, display_name, preferred_language, is_client, state, created_at, row_version
FROM party.contact WHERE id = $1::uuid`

// Get implements Contacts. An id that is not a uuid is not found.
func (s *Store) Get(ctx context.Context, id ContactID) (Contact, error) {
	if id == "" {
		return Contact{}, fmt.Errorf("get contact: %w", ErrNotFound)
	}
	var out Contact
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var cid, kind, state string
		if err := tx.QueryRow(ctx, getContactSQL, string(id)).Scan(
			&cid, &kind, &out.LegalName, &out.DisplayName, &out.PreferredLanguage, &out.IsClient, &state, &out.CreatedAt, &out.RowVersion,
		); err != nil {
			var pg *pgconn.PgError
			if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
				return ErrNotFound
			}
			return fmt.Errorf("select: %w", err)
		}
		out.ID = ContactID(cid)
		out.Kind = Kind(kind)
		out.State = State(state)
		return nil
	})
	if err != nil {
		return Contact{}, fmt.Errorf("get contact %s: %w", id, err)
	}
	return out, nil
}

const searchContactsSQL = `SELECT id::text, kind, legal_name, display_name, preferred_language, is_client, state, created_at, row_version
FROM party.contact
WHERE state <> 'merged'
  AND (lower(legal_name) LIKE $1 ESCAPE '\' OR lower(display_name) LIKE $1 ESCAPE '\')
ORDER BY lower(display_name), id
LIMIT $2`

// likePattern is a case-folded LIKE pattern matching query anywhere, with LIKE wildcards in
// query matched literally.
func likePattern(query string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.ToLower(strings.TrimSpace(query))) + "%"
}

// Search implements Contacts. The query is client data: errors never echo it.
func (s *Store) Search(ctx context.Context, query string) ([]Contact, error) {
	out := []Contact{}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, searchContactsSQL, likePattern(query), SearchLimit)
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c Contact
			var kind, state string
			if err := rows.Scan(&c.ID, &kind, &c.LegalName, &c.DisplayName, &c.PreferredLanguage, &c.IsClient, &state, &c.CreatedAt, &c.RowVersion); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			c.Kind, c.State = Kind(kind), State(state)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("search contacts: %w", err)
	}
	return out, nil
}

const listContactsSQL = `SELECT id::text, kind, legal_name, display_name, preferred_language, is_client, state, created_at, row_version
FROM party.contact
WHERE state <> 'merged' AND ($1 = '' OR id > NULLIF($1, '')::uuid)
ORDER BY id
LIMIT $2`

// List implements Contacts. Ids are uuidv7, so id order is creation order.
func (s *Store) List(ctx context.Context, f Filter) ([]Contact, error) {
	out := []Contact{}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listContactsSQL, string(f.After), f.PageSize())
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c Contact
			var kind, state string
			if err := rows.Scan(&c.ID, &kind, &c.LegalName, &c.DisplayName, &c.PreferredLanguage, &c.IsClient, &state, &c.CreatedAt, &c.RowVersion); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			c.Kind, c.State = Kind(kind), State(state)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	return out, nil
}
