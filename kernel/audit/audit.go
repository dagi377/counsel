package audit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ Log = (*Postgres)(nil)

// Querier is the read side Verify needs: a pool or a connection.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Options configure how an Event maps onto audit.audit_event.
type Options struct {
	// Streams maps an action prefix (the text before the first dot) to a stream; actions
	// with an unmapped prefix go to DefaultStream.
	Streams map[string]string
	// ActorKinds maps an Event.ActorKind to the stored actor_kind; unmapped kinds are stored as is.
	ActorKinds map[string]string
	// CanonicalSQL is the SQL expression (bytea, over the columns of audit.audit_event) that the
	// audit.chain trigger hashes. The app that owns the table's columns supplies it at boot, and
	// it must match the trigger exactly. Verify fails with ErrNoCanonical without it.
	CanonicalSQL string
}

// ErrNoCanonical means Verify was called on a Log configured without Options.CanonicalSQL.
var ErrNoCanonical = errors.New("audit: no canonical row expression configured")

// DefaultStream receives actions whose prefix has no stream.
const DefaultStream = "platform"

// Postgres is the production Log. The audit.chain trigger assigns seq, prev_hash and hash
// under an advisory lock; Verify recomputes every hash in Go from the canonical row.
type Postgres struct {
	db     Querier
	masker Masker
	opts   Options
}

// New returns a Log that reads through db for Verify and masks detail with m (nil keeps
// detail as given; callers must then pass only safe values).
func New(db Querier, m Masker, opts Options) *Postgres {
	return &Postgres{db: db, masker: m, opts: opts}
}

// rule: ARC-I-043
// Append writes e in the caller's transaction; the row commits or rolls back with the change
// it records.
func (p *Postgres) Append(ctx context.Context, tx pgx.Tx, e Event) error {
	if e.ActorKind == "" || e.ActorID == "" || e.Action == "" {
		return fmt.Errorf("append %q: %w", e.Action, ErrInvalidEvent)
	}
	detail := e.Detail
	if p.masker != nil {
		detail = p.masker.Mask(detail)
	}
	if detail == nil {
		detail = map[string]string{}
	}
	kind := e.ActorKind
	if k, ok := p.opts.ActorKinds[kind]; ok {
		kind = k
	}
	var actor, run, obo *string
	switch e.ActorKind {
	case "agent":
		run, obo = &e.ActorID, nonEmpty(e.ActingFor)
	case "system":
	default:
		actor, obo = &e.ActorID, nonEmpty(e.ActingFor)
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit.audit_event
  (stream, actor_kind, actor_user_id, on_behalf_of, agent_run_id, action, object_type, object_id, detail_masked)
  VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, $9)`,
		p.stream(e.Action), kind, actor, obo, run, e.Action, e.Subject.Kind, e.Subject.ID, detail)
	if err != nil {
		return fmt.Errorf("append %q: %w", e.Action, mapErr(err))
	}
	return nil
}

// Verify recomputes the chain from..to inclusive.
func (p *Postgres) Verify(ctx context.Context, from, to Seq) error {
	if from < 1 || from > to {
		return fmt.Errorf("verify %d..%d: %w", from, to, ErrBadRange)
	}
	if p.opts.CanonicalSQL == "" {
		return fmt.Errorf("verify %d..%d: %w", from, to, ErrNoCanonical)
	}
	var last Seq
	if err := p.db.QueryRow(ctx, `SELECT coalesce(max(seq), 0) FROM audit.audit_event`).Scan(&last); err != nil {
		return fmt.Errorf("verify %d..%d: last seq: %w", from, to, err)
	}
	if to > last {
		return fmt.Errorf("verify %d..%d: last is %d: %w", from, to, last, ErrBadRange)
	}
	prev := Genesis
	if from > 1 {
		if err := p.db.QueryRow(ctx, `SELECT hash FROM audit.audit_event WHERE seq = $1`, from-1).Scan(&prev); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("verify seq %d missing: %w", from-1, ErrChainBroken)
			}
			return fmt.Errorf("verify %d..%d: anchor: %w", from, to, err)
		}
	}
	rows, err := p.db.Query(ctx, `SELECT seq, prev_hash, hash, `+p.opts.CanonicalSQL+`
  FROM audit.audit_event WHERE seq BETWEEN $1 AND $2 ORDER BY seq`, from, to)
	if err != nil {
		return fmt.Errorf("verify %d..%d: %w", from, to, err)
	}
	chain, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChainRow, error) {
		var c ChainRow
		err := r.Scan(&c.Seq, &c.PrevHash, &c.Hash, &c.Canonical)
		return c, err
	})
	if err != nil {
		return fmt.Errorf("verify %d..%d: read: %w", from, to, err)
	}
	if len(chain) == 0 || chain[0].Seq != from || chain[len(chain)-1].Seq != to {
		return fmt.Errorf("verify %d..%d: rows missing: %w", from, to, ErrChainBroken)
	}
	return VerifyChain(prev, chain)
}

func (p *Postgres) stream(action string) string {
	prefix, _, _ := strings.Cut(action, ".")
	if s, ok := p.opts.Streams[prefix]; ok {
		return s
	}
	return DefaultStream
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// rule: BIL-I-003
// mapErr turns the append-only trigger's refusal into ErrImmutable.
func mapErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23001" { // restrict_violation
		return fmt.Errorf("%w: %s", ErrImmutable, pgErr.Code)
	}
	return err
}
