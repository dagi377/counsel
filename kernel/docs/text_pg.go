package docs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// SQL PGText runs.
const (
	versionTextSQL    = `SELECT body FROM docs.version_text WHERE version_id = $1::uuid`
	versionTextPutSQL = `INSERT INTO docs.version_text (version_id, body) VALUES ($1::uuid, $2)
  ON CONFLICT (version_id) DO NOTHING`
)

// PGText reads the citable text of a stored version from docs.version_text. A version's text
// is stored once and never changes, so a citation keeps verifying against what it quoted.
type PGText struct {
	DB db.DB
}

// Put stores the extracted text of v in tx. A version that already has text keeps it.
func (p PGText) Put(ctx context.Context, tx pgx.Tx, v VersionID, body []byte) error {
	if _, err := tx.Exec(ctx, versionTextPutSQL, string(v), string(body)); err != nil {
		return fmt.Errorf("put text %s: %w", v, err)
	}
	return nil
}

// text loads and parses the stored text of v; a version with no stored text is ErrNotFound.
func (p PGText) text(ctx context.Context, v VersionID) (Text, error) {
	if p.DB == nil {
		return Text{}, fmt.Errorf("version %s: no database: %w", v, ErrNotFound)
	}
	var body string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, versionTextSQL, string(v)).Scan(&body)
	})
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.As(err, &pg) && pg.Code == "22P02":
		return Text{}, fmt.Errorf("version %s: %w", v, ErrNotFound)
	case err != nil:
		return Text{}, fmt.Errorf("version %s: %w", v, err)
	}
	return ParseText(v, []byte(body)), nil
}

// Lines returns the lines of one page of v.
func (p PGText) Lines(ctx context.Context, v VersionID, page int) ([]Line, error) {
	t, err := p.text(ctx, v)
	if err != nil {
		return nil, fmt.Errorf("lines: %w", err)
	}
	ls, err := t.Page(page)
	if err != nil {
		return nil, fmt.Errorf("lines: %w", err)
	}
	return ls, nil
}

// Cite returns a citation for lines l0..l1 of a page of v.
func (p PGText) Cite(ctx context.Context, v VersionID, page, l0, l1 int) (Citation, error) {
	t, err := p.text(ctx, v)
	if err != nil {
		return Citation{}, fmt.Errorf("cite: %w", err)
	}
	// rule: ARC-I-037
	return t.Cite(page, l0, l1)
}

// Verify re-hashes the cited lines against v's stored text.
func (p PGText) Verify(ctx context.Context, c Citation) error {
	t, err := p.text(ctx, c.VersionID)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	// rule: ARC-I-037
	return t.Verify(c)
}
