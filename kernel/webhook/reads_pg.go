package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// PGEndpointReads is the Postgres EndpointReads over platform.webhook. Version is the row's
// row_version, which the PATCH route compares with If-Match.
type PGEndpointReads struct {
	DB db.DB
}

var _ EndpointReads = PGEndpointReads{}

// Statements PGEndpointReads issues. Neither selects secret_ref.
const (
	sqlEndpointPage = `SELECT id::text, url, events, status, expires_at, row_version FROM platform.webhook
WHERE id::text > $1 ORDER BY id::text LIMIT $2`
	sqlEndpointOne = `SELECT id::text, url, events, status, expires_at, row_version FROM platform.webhook
WHERE id::text = $1`
)

// Endpoints returns the endpoints whose id sorts after f.After, at most f.Limit, in id order.
func (s PGEndpointReads) Endpoints(ctx context.Context, f EndpointFilter) ([]EndpointInfo, error) {
	var out []EndpointInfo
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlEndpointPage, f.After, f.Limit)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEndpoint(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("rows: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list webhook endpoints: %w", err)
	}
	return out, nil
}

// Endpoint returns the endpoint id, or ErrUnknownEndpoint.
func (s PGEndpointReads) Endpoint(ctx context.Context, id string) (EndpointInfo, error) {
	var e EndpointInfo
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		e, err = scanEndpoint(tx.QueryRow(ctx, sqlEndpointOne, id))
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return EndpointInfo{}, fmt.Errorf("webhook endpoint %s: %w", id, ErrUnknownEndpoint)
	case err != nil:
		return EndpointInfo{}, fmt.Errorf("webhook endpoint %s: %w", id, err)
	}
	return e, nil
}

func scanEndpoint(row pgx.Row) (EndpointInfo, error) {
	var e EndpointInfo
	var exp *time.Time
	if err := row.Scan(&e.ID, &e.URL, &e.Events, &e.Status, &exp, &e.Version); err != nil {
		return EndpointInfo{}, err
	}
	e.ExpiresAt = exp
	return e, nil
}
