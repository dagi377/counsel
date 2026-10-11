package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Exec runs one statement outside any transaction, for relays and sweeps that own their rows
// (the outbox relay). Request work goes through InTx so its session is bound.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if p.isClosed() {
		return pgconn.CommandTag{}, fmt.Errorf("exec: %w", ErrClosed)
	}
	return p.pool.Exec(ctx, sql, args...)
}

// Query runs one query outside any transaction; see Exec.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if p.isClosed() {
		return nil, fmt.Errorf("query: %w", ErrClosed)
	}
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow runs one single-row query outside any transaction; see Exec. On a closed pool the
// row's Scan reports ErrClosed.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if p.isClosed() {
		return closedRow{}
	}
	return p.pool.QueryRow(ctx, sql, args...)
}

type closedRow struct{}

func (closedRow) Scan(...any) error { return fmt.Errorf("query row: %w", ErrClosed) }
