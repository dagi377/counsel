package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sessionZone is set with SET LOCAL in every transaction so timestamptz values render the
// same text whatever the connection's default zone. The audit.chain trigger hashes
// jsonb text that includes `at`, and audit verification recomputes it; both must agree
// (Q-004). Business dates still use America/Toronto through the clock package.
const setZone = "SET LOCAL TIME ZONE 'UTC'"

var _ DB = (*Pool)(nil)

// Pool is the pgx-backed DB.
type Pool struct {
	pool *pgxpool.Pool
}

// Open connects a pool to dsn.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db open: parse config: %w", err)
	}
	return OpenConfig(ctx, cfg)
}

// OpenConfig connects a pool from a parsed config.
func OpenConfig(ctx context.Context, cfg *pgxpool.Config) (*Pool, error) {
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	return &Pool{pool: p}, nil
}

// InTx implements DB. The transaction's time zone is UTC (Q-004).
func (p *Pool) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) (err error) {
	if p.pool == nil {
		return ErrClosed
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db begin: %w", err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("db rollback: %w", rbErr))
			}
		}
	}()
	if _, err = tx.Exec(ctx, setZone); err != nil {
		return fmt.Errorf("db set time zone: %w", err)
	}
	if err = fn(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db commit: %w", err)
	}
	return nil
}

// Bind implements DB: it sets UTC and the session variables with SET LOCAL.
func (p *Pool) Bind(ctx context.Context, tx pgx.Tx, s Session) error {
	if tx == nil {
		return ErrNoTx
	}
	if _, err := tx.Exec(ctx, setZone); err != nil {
		return fmt.Errorf("db bind time zone: %w", err)
	}
	for k, v := range s.Vars() {
		if _, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", k, v); err != nil {
			return fmt.Errorf("db bind %s: %w", k, err)
		}
	}
	return nil
}

// Ping implements DB.
func (p *Pool) Ping(ctx context.Context) error {
	if p.pool == nil {
		return ErrClosed
	}
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}
	return nil
}

// Close implements DB.
func (p *Pool) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}
