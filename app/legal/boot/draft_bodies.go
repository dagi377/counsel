package boot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/kernel/db"
)

// DraftBodyRefPrefix starts every reference PGDraftBodies returns: "agent.draft_body:<uuid>".
const DraftBodyRefPrefix = "agent.draft_body:"

// ErrDraftBodyNotFound: no stored draft body has this reference.
var ErrDraftBodyNotFound = errors.New("legal boot: draft body not found")

// BodySealer seals and opens draft bodies; *trust.AESSealer, over the boot seal key, is one.
type BodySealer interface {
	Seal(ctx context.Context, plaintext []byte) ([]byte, error)
	Open(ctx context.Context, sealed []byte) ([]byte, error)
}

// PGDraftBodies stores the body of an agent draft in agent.draft_body, sealed with AES-256-GCM.
// The draft row (docs.draft, comms.comm_item, matter.note) holds only the reference, and a
// retried draft call reuses its row through the draft's idempotency key (Q-DRAFT-FIX), so it
// stores no second draft. No statement, log line or error carries the text.
type PGDraftBodies struct {
	DB     db.DB
	Sealer BodySealer
}

const (
	sqlPutDraftBody = `INSERT INTO agent.draft_body (sealed) VALUES ($1) RETURNING id::text`
	sqlGetDraftBody = `SELECT sealed FROM agent.draft_body WHERE id = $1::uuid`
)

// Put implements matter.Bodies: it seals text and returns the stored body's reference.
func (s PGDraftBodies) Put(ctx context.Context, text string) (string, error) {
	if s.DB == nil || s.Sealer == nil {
		return "", fmt.Errorf("draft body store: %w", agent.ErrMissingDep)
	}
	// rule: ARC-U-040
	// The text is sealed before it leaves this process; the row holds ciphertext only.
	sealed, err := s.Sealer.Seal(ctx, []byte(text))
	if err != nil {
		return "", fmt.Errorf("draft body: seal: %w", err)
	}
	var id string
	err = s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlPutDraftBody, sealed).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("draft body: store: %w", err)
	}
	return DraftBodyRefPrefix + id, nil
}

// Get opens the body ref names. A ref PGDraftBodies did not return is ErrDraftBodyNotFound.
func (s PGDraftBodies) Get(ctx context.Context, ref string) (string, error) {
	if s.DB == nil || s.Sealer == nil {
		return "", fmt.Errorf("draft body store: %w", agent.ErrMissingDep)
	}
	id, ok := strings.CutPrefix(ref, DraftBodyRefPrefix)
	if !ok || !isUUID(id) {
		return "", fmt.Errorf("draft body: %w", ErrDraftBodyNotFound)
	}
	var sealed []byte
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sqlGetDraftBody, id).Scan(&sealed)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("draft body %s: %w", id, ErrDraftBodyNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("draft body %s: %w", id, err)
	}
	text, err := s.Sealer.Open(ctx, sealed)
	if err != nil {
		return "", fmt.Errorf("draft body %s: open: %w", id, err)
	}
	return string(text), nil
}
