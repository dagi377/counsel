// Package mattertest seeds matter rows for other modules' integration tests. The matter
// module owns its tables (ARC-I-041), so other modules seed matters through this package
// instead of writing matter SQL themselves.
package mattertest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Seed inserts one open matter with the given number and returns its id.
func Seed(ctx context.Context, tx pgx.Tx, number, shortName, clientID, practiceAreaID, lawyerID string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO matter.matter(number, short_name, client_id, practice_area_id, responsible_lawyer_id)
VALUES ($1, $2, $3::uuid, $4::uuid, $5::uuid) RETURNING id::text`, number, shortName, clientID, practiceAreaID, lawyerID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("seed matter: %w", err)
	}
	return id, nil
}
