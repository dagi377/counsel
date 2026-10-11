package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setZone is set with SET LOCAL in every transaction so timestamptz values render the
// same text whatever the connection's default zone. The audit.chain trigger hashes
// jsonb text that includes `at`, and audit verification recomputes it; both must agree
// (Q-004). Business dates still use America/Toronto through the clock package.
const setZone = "SET LOCAL TIME ZONE 'UTC'"

var _ DB = (*Pool)(nil)

// Config names the two ways into Postgres. PooledDSN goes through the transaction-mode
// pooler and serves request transactions. DirectDSN goes to the Postgres rw service (or a
// session-mode pool) and serves the job queue, LISTEN/NOTIFY, advisory locks and migrations.
type Config struct {
	PooledDSN string
	DirectDSN string
	// MaxConns caps the pooled connections; 0 keeps the pgx default.
	MaxConns int32
}

// Validate checks that both DSNs are set and that the direct one is not the pooler.
func (c Config) Validate() error {
	switch {
	case c.PooledDSN == "":
		return errors.New("db config: pooled dsn is empty")
	case c.DirectDSN == "":
		return errors.New("db config: direct dsn is empty")
	// rule: ARC-U-011
	case c.DirectDSN == c.PooledDSN:
		return errors.New("db config: direct dsn must not be the transaction-mode pooler")
	}
	return nil
}

// Pool is the request pool. Session state is bound per transaction with SET LOCAL only, so a
// pooled server connection carries nothing from one request into the next.
type Pool struct {
	pool   *pgxpool.Pool
	closed atomic.Bool
}

// Open connects the request pool through cfg.PooledDSN.
func Open(ctx context.Context, cfg Config) (*Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	pc, err := pgxpool.ParseConfig(cfg.PooledDSN)
	if err != nil {
		return nil, fmt.Errorf("db open: parse pooled dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	// Transaction-mode pooling cannot keep server-side prepared statements across requests.
	pc.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	p, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	return &Pool{pool: p}, nil
}

// OpenConfig connects a request pool from an already parsed config. It skips the DSN
// checks in Config.Validate; callers that know their target (tests) use it.
func OpenConfig(ctx context.Context, cfg *pgxpool.Config) (*Pool, error) {
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	return &Pool{pool: p}, nil
}

// OpenDirect connects through cfg.DirectDSN, bypassing the transaction-mode pooler. The job
// queue client and migrations use this pool.
func OpenDirect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// rule: ARC-U-011
	p, err := pgxpool.New(ctx, cfg.DirectDSN)
	if err != nil {
		return nil, fmt.Errorf("db open direct: %w", err)
	}
	return p, nil
}

// poolTx marks a transaction as begun by a specific Pool.
type poolTx struct {
	pgx.Tx
	owner *Pool
}

// InTx begins a transaction, runs fn, and commits when fn returns nil.
func (p *Pool) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if p.isClosed() {
		return fmt.Errorf("begin: %w", ErrClosed)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if _, err := tx.Exec(ctx, setZone); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(fmt.Errorf("set time zone: %w", err), fmt.Errorf("rollback: %w", rbErr))
		}
		return fmt.Errorf("set time zone: %w", err)
	}
	if err := fn(ctx, &poolTx{Tx: tx, owner: p}); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Bind sets UTC and s on tx with set_config(name, value, true), the function form of SET LOCAL.
func (p *Pool) Bind(ctx context.Context, tx pgx.Tx, s Session) error {
	t, ok := tx.(*poolTx)
	if !ok || t == nil || t.owner != p {
		return fmt.Errorf("bind: %w", ErrNoTx)
	}
	if _, err := t.Exec(ctx, setZone); err != nil {
		return fmt.Errorf("bind time zone: %w", err)
	}
	vars := s.Vars()
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		// rule: ARC-U-011
		if _, err := t.Exec(ctx, "SELECT set_config($1, $2, true)", name, vars[name]); err != nil {
			return fmt.Errorf("bind %s: %w", name, err)
		}
	}
	return nil
}

// Ping checks the database is reachable.
func (p *Pool) Ping(ctx context.Context) error {
	if p.isClosed() {
		return fmt.Errorf("ping: %w", ErrClosed)
	}
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// Close releases the pool.
func (p *Pool) Close() {
	if p.pool != nil && p.closed.CompareAndSwap(false, true) {
		p.pool.Close()
	}
}

func (p *Pool) isClosed() bool {
	return p.pool == nil || p.closed.Load()
}
