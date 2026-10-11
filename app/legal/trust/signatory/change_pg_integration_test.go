//go:build integration

package signatory

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
)

// TestPGServiceChangeIntegration proposes and approves a signatory change on the real schema:
// self-approval is refused, approval grants the user, and approving again changes nothing.
func TestPGServiceChangeIntegration(t *testing.T) {
	pool := directoryDB(t)
	ctx := context.Background()
	today := civil(clock.Today(ctx))
	ids := map[string]string{}
	var account string
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, `INSERT INTO core.role(key, name, status) VALUES ('staff', 'Staff', 'active') RETURNING id::text`).Scan(&role); err != nil {
			return err
		}
		for _, u := range []string{"lic1", "lic2"} {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id, licence_type, licence_status, principal_kind, status)
VALUES ($1 || '@example.com', $1, $2::uuid, 'lawyer', 'good_standing', 'human', 'active') RETURNING id::text`, u, role).Scan(&id); err != nil {
				return err
			}
			ids[u] = id
		}
		if err := tx.QueryRow(ctx, `INSERT INTO trust.trust_account(type, institution, account_no_ciphertext, account_no_last4, title, opened_on)
VALUES ('mixed', 'Bank', '\x00', '1234', 'General trust', $1::date - 30) RETURNING id::text`, today).Scan(&account); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO trust.signatory(account_id, user_id, is_licensee, valid_from, approval_id)
VALUES ($1::uuid, $2::uuid, true, $3::date - 1, core.uuidv7())`, account, ids["lic1"], today)
		return err
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewPGService(pool)
	c, err := s.Propose(ctx, Change{AccountID: account, PreparedBy: ids["lic1"],
		Add: []Signatory{{AccountID: account, UserID: ids["lic2"], Kind: Licensee}}})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	for _, tc := range []struct {
		name string
		by   string
		want error
	}{
		{"self-approval", ids["lic1"], ErrSelfApproval},
		{"approve", ids["lic2"], nil},
		{"approve again", ids["lic2"], nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Approve(ctx, c.ID, Approver{UserID: tc.by}); !errors.Is(err, tc.want) {
				t.Fatalf("Approve err = %v, want %v", err, tc.want)
			}
		})
	}
	if ok, err := s.CanSign(ctx, account, ids["lic2"]); err != nil || !ok {
		t.Fatalf("lic2 CanSign = %v, %v; want true", ok, err)
	}
	if _, err := s.Approve(ctx, "00000000-0000-7000-8000-000000000001", Approver{UserID: ids["lic2"]}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown change err = %v, want ErrNotFound", err)
	}
}
