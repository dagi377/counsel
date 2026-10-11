package signatory

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/registry"
)

// changeDB plays trust.signatory_change, trust.signatory and core.app_user for the
// statements Propose and Approve run.
type changeDB struct {
	db.DB
	users   map[string]User
	sigs    []*tableRow
	changes map[string]*changeRow
	grants  []string // approval_id of every inserted grant
	writes  int      // every Exec that changed a row
}

func newChangeDB() *changeDB {
	return &changeDB{
		users: map[string]User{
			"lic1":  {ID: "lic1", Licensee: true, Active: true},
			"lic2":  {ID: "lic2", Licensee: true, Active: true},
			"clerk": {ID: "clerk", Active: true},
			"gone":  {ID: "gone", Licensee: true},
		},
		sigs:    []*tableRow{{account: "a1", row: row{UserID: "lic1", Licensee: true, ValidFrom: yesterday}}},
		changes: map[string]*changeRow{},
	}
}

func (d *changeDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, changeTx{d: d})
}

type changeTx struct {
	pgx.Tx
	d *changeDB
}

type fnRow func(dest ...any) error

func (f fnRow) Scan(dest ...any) error { return f(dest...) }

func (t changeTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch sql {
	case directoryUserSQL:
		u, ok := t.d.users[args[0].(string)]
		return fnRow(func(dest ...any) error {
			if !ok {
				return pgx.ErrNoRows
			}
			*dest[0].(*string), *dest[1].(*bool), *dest[2].(*bool) = u.ID, u.Licensee, u.Active
			return nil
		})
	case insertChangeSQL:
		id := fmt.Sprintf("c%d", len(t.d.changes)+1)
		t.d.changes[id] = &changeRow{ID: id, AccountID: args[0].(string), UserID: args[1].(string), Op: args[2].(string),
			Kind: args[3].(string), BondEvidence: args[4].(string), ProposedBy: args[5].(string), Status: "pending"}
		return fnRow(func(dest ...any) error { *dest[0].(*string) = id; return nil })
	case lockChangeSQL:
		c, ok := t.d.changes[args[0].(string)]
		return fnRow(func(dest ...any) error {
			if !ok {
				return pgx.ErrNoRows
			}
			for i, v := range []string{c.ID, c.AccountID, c.UserID, c.Op, c.Kind, c.BondEvidence, c.ProposedBy, c.ApprovedBy} {
				*dest[i].(*string) = v
			}
			*dest[8].(**time.Time) = c.ApprovedAt
			*dest[9].(*string) = c.Status
			return nil
		})
	}
	return fnRow(func(...any) error { return fmt.Errorf("unexpected sql %q", sql) })
}

func (t changeTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if sql != lockSignatoriesSQL {
		return nil, fmt.Errorf("unexpected sql %q", sql)
	}
	account, today := args[0].(string), args[1].(time.Time)
	var out []row
	for _, r := range t.d.sigs {
		if r.account == account && current(r, today) {
			out = append(out, r.row)
		}
	}
	return &tableRows{rows: out, i: -1}, nil
}

func (t changeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch sql {
	case insertSignatorySQL:
		t.d.sigs = append(t.d.sigs, &tableRow{account: args[0].(string), row: row{
			UserID: args[1].(string), Licensee: args[2].(bool), Bonded: args[3] != (*int64)(nil), ValidFrom: args[4].(time.Time)}})
		t.d.grants = append(t.d.grants, args[5].(string))
	case endSignatorySQL:
		for _, r := range t.d.sigs {
			if r.account == args[0].(string) && r.UserID == args[1].(string) && r.ValidTo == nil {
				end := args[2].(time.Time)
				r.ValidTo = &end
			}
		}
	case markApprovedSQL:
		c := t.d.changes[args[0].(string)]
		at := args[3].(time.Time)
		c.Status, c.ApprovedBy, c.ApprovedAt = "approved", args[1].(string), &at
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected sql %q", sql)
	}
	t.d.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func addChange(user string, kind Kind, by string) Change {
	return Change{AccountID: "a1", PreparedBy: by, Add: []Signatory{{AccountID: "a1", UserID: user, Kind: kind}}}
}

func TestPGServiceProposeApprove(t *testing.T) {
	ctx := todayCtx(t)
	tests := []struct {
		name        string
		change      Change
		by          Approver
		wantPropose error
		wantApprove error
		wantLic2    bool
	}{
		{"licensee added", addChange("lic2", Licensee, "clerk"), Approver{UserID: "lic1"}, nil, nil, true},
		{"self-approval", addChange("lic2", Licensee, "lic1"), Approver{UserID: "lic1"}, nil, ErrSelfApproval, false},
		{"agent approver", addChange("lic2", Licensee, "clerk"), Approver{UserID: "lic1", IsAgent: true}, nil, ErrAgentApproval, false},
		{"non-licensee approver", addChange("lic2", Licensee, "lic1"), Approver{UserID: "clerk"}, nil, ErrNotEligible, false},
		{"non-licensee as licensee", addChange("clerk", Licensee, "lic1"), Approver{}, ErrNotEligible, nil, false},
		{"bonded without evidence", addChange("clerk", BondedNonLicensee, "lic1"), Approver{}, ErrNotEligible, nil, false},
		{"departed user", addChange("gone", Licensee, "lic1"), Approver{}, ErrNotEligible, nil, false},
		{"remove last signatory", Change{AccountID: "a1", PreparedBy: "clerk", Remove: []string{"lic1"}}, Approver{}, ErrLastSignatory, nil, false},
		{"two users in one change", Change{AccountID: "a1", PreparedBy: "clerk", Remove: []string{"lic1", "lic2"}}, Approver{}, ErrInvalidState, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newChangeDB()
			s := NewPGService(d)
			c, err := s.Propose(ctx, tt.change)
			if !errors.Is(err, tt.wantPropose) {
				t.Fatalf("Propose err = %v, want %v", err, tt.wantPropose)
			}
			if err != nil {
				if d.writes != 0 || len(d.changes) != 0 {
					t.Fatalf("refused proposal wrote: %d writes, %d changes", d.writes, len(d.changes))
				}
				return
			}
			if c.Status != ChangePending || c.ID == "" {
				t.Fatalf("proposed = %+v, want pending with id", c)
			}
			got, err := s.Approve(ctx, c.ID, tt.by)
			if !errors.Is(err, tt.wantApprove) {
				t.Fatalf("Approve err = %v, want %v", err, tt.wantApprove)
			}
			if err != nil && (d.writes != 0 || d.changes[c.ID].Status != "pending") {
				t.Fatalf("refused approval wrote: %d writes, change %+v", d.writes, d.changes[c.ID])
			}
			if err == nil && (got.Status != ChangeApproved || got.ApprovedBy != tt.by.UserID) {
				t.Fatalf("approved = %+v", got)
			}
			if has := len(d.sigs) == 2; has != tt.wantLic2 {
				t.Fatalf("lic2 granted = %v, want %v", has, tt.wantLic2)
			}
		})
	}
}

func TestPGServiceApproveTwiceChangesNothing(t *testing.T) {
	ctx := todayCtx(t)
	d := newChangeDB()
	s := NewPGService(d)
	c, err := s.Propose(ctx, addChange("lic2", Licensee, "clerk"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Approve(ctx, c.ID, Approver{UserID: "lic1"})
	if err != nil {
		t.Fatal(err)
	}
	writes := d.writes
	tests := []struct {
		name string
		by   Approver
	}{
		{"same approver", Approver{UserID: "lic1"}},
		{"other approver", Approver{UserID: "lic2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			again, err := s.Approve(ctx, c.ID, tt.by)
			if err != nil || d.writes != writes || again.ApprovedBy != first.ApprovedBy || len(d.sigs) != 2 {
				t.Fatalf("second approve = %+v, %v; writes %d -> %d", again, err, writes, d.writes)
			}
		})
	}
}

func TestPGServiceApproveUnknown(t *testing.T) {
	ctx := todayCtx(t)
	s := NewPGService(newChangeDB())
	tests := []struct{ name, id string }{{"unknown", "c99"}, {"empty", ""}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Approve(ctx, tt.id, Approver{UserID: "lic1"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

// TestTxEffectsOnGateTx applies a GA6 gate through TxEffects: the change runs on the gate tx
// and the grant carries the gate id as its approval.
func TestTxEffectsOnGateTx(t *testing.T) {
	ctx := todayCtx(t)
	d := newChangeDB()
	// The service's own pool must never be used: every statement goes to the gate tx.
	s := NewPGService(&tableDB{err: errors.New("pool used outside the gate tx")})
	tests := []struct {
		name    string
		payload string
		want    error
	}{
		{"remove last signatory", `{"trust_account_id":"a1","user_id":"lic1","change":"remove"}`, ErrLastSignatory},
		{"add licensee", `{"trust_account_id":"a1","user_id":"lic2","change":"add"}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := registry.Approved{GateID: "gate-1", Kind: KindChange, Payload: []byte(tt.payload),
				Preparer:  registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk"},
				Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: "lic1"}}}
			before := len(d.grants)
			err := TxEffects(s)[KindChange](ctx, changeTx{d: d}, g)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && (len(d.grants) != before+1 || d.grants[before] != "gate-1") {
				t.Fatalf("grants = %v, want one under gate-1", d.grants)
			}
		})
	}
	if _, ok := TxEffects(s)[KindSigningAuthority]; !ok {
		t.Fatal("alias kind missing from TxEffects")
	}
}
