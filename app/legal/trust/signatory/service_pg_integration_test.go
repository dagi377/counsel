//go:build integration

package signatory

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
)

func TestPGServiceIntegration(t *testing.T) {
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
		for _, u := range []string{"lic1", "lic2", "clerk"} {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id, licence_type, licence_status, principal_kind, status)
VALUES ($1 || '@example.com', $1, $2::uuid, 'none', 'n/a', 'human', 'active') RETURNING id::text`, u, role).Scan(&id); err != nil {
				return err
			}
			ids[u] = id
		}
		if err := tx.QueryRow(ctx, `INSERT INTO trust.trust_account(type, institution, account_no_ciphertext, account_no_last4, title, opened_on)
VALUES ('mixed', 'Bank', '\x00', '1234', 'General trust', $1::date - 30) RETURNING id::text`, today).Scan(&account); err != nil {
			return err
		}
		for _, s := range []struct {
			user, cols string
		}{
			{"lic1", `true, NULL, NULL, $3::date - 1, NULL`},
			{"lic2", `true, NULL, NULL, $3::date + 1, NULL`},
			{"clerk", `false, 100000, $3::date - 1, $3::date - 5, NULL`},
		} {
			if _, err := tx.Exec(ctx, `INSERT INTO trust.signatory(account_id, user_id, is_licensee, bond_cents, bond_expiry, valid_from, valid_to, approval_id)
VALUES ($1::uuid, $2::uuid, `+s.cols+`, core.uuidv7())`, account, ids[s.user], today); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := NewPGService(pool)
	for _, tc := range []struct {
		name string
		user string
		want bool
	}{
		{"licensee valid today", ids["lic1"], true},
		{"licensee starts tomorrow", ids["lic2"], false},
		{"bond expired", ids["clerk"], false},
		{"malformed id", "not-a-uuid", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.CanSign(ctx, account, tc.user)
			if err != nil || got != tc.want {
				t.Fatalf("CanSign = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	list, err := s.List(ctx, account)
	if err != nil || len(list) != 3 {
		t.Fatalf("List = %+v, %v; want 3 rows", list, err)
	}
	n, err := s.Freeze(ctx, ids["lic1"])
	if err != nil || n != 1 {
		t.Fatalf("Freeze = %d, %v; want 1", n, err)
	}
	if ok, err := s.CanSign(ctx, account, ids["lic1"]); err != nil || ok {
		t.Fatalf("frozen lic1 CanSign = %v, %v", ok, err)
	}
	if n, err := s.Freeze(ctx, ids["lic1"]); err != nil || n != 0 {
		t.Fatalf("second Freeze = %d, %v; want 0", n, err)
	}
}
