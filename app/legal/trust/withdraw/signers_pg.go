package withdraw

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
)

// PGSigners is the Postgres SignatoryCheck over trust.signatory. It reads in the caller's tx
// (the gate tx) with the signatory module's own validity rule, so a withdrawal and the
// signatory list never disagree on who can sign.
type PGSigners struct{}

var _ SignatoryCheck = PGSigners{}

// IsSignatory implements SignatoryCheck: true only when userID holds a grant on account that
// is valid today and is a licensee or holds an unexpired bond.
func (PGSigners) IsSignatory(ctx context.Context, tx pgx.Tx, account trust.AccountID, userID string) (bool, error) {
	ok, err := signatory.CanSignTx(ctx, tx, string(account), userID)
	if err != nil {
		return false, fmt.Errorf("withdraw signers: %w", err)
	}
	return ok, nil
}
