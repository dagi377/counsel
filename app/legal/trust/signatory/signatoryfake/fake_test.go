package signatoryfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/app/legal/trust/signatory"
)

// seeded returns a Fake with account "a1" signed by licensee "lic1".
func seeded(t *testing.T) *Fake {
	t.Helper()
	ctx := context.Background()
	f := New()
	for _, u := range []signatory.User{
		{ID: "lic1", Licensee: true, Active: true},
		{ID: "lic2", Licensee: true, Active: true},
		{ID: "lic3", Licensee: true, Active: true},
		{ID: "clerk", Active: true},
		{ID: "gone", Licensee: true},
	} {
		f.PutUser(u)
	}
	c, err := f.Propose(ctx, signatory.Change{AccountID: "a1", Add: []signatory.Signatory{{UserID: "lic1", Kind: signatory.Licensee}}, PreparedBy: "lic2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Approve(ctx, c.ID, signatory.Approver{UserID: "lic3"}); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestProposeApprove(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name       string
		change     signatory.Change
		by         signatory.Approver
		proposeErr error
		approveErr error
		wantSigner string
	}{
		{name: "add licensee", change: signatory.Change{Add: []signatory.Signatory{{UserID: "lic2", Kind: signatory.Licensee}}, PreparedBy: "lic1"}, by: signatory.Approver{UserID: "lic3"}, wantSigner: "lic2"},
		{name: "add bonded clerk", change: signatory.Change{Add: []signatory.Signatory{{UserID: "clerk", Kind: signatory.BondedNonLicensee, BondEvidence: "doc-1"}}, PreparedBy: "lic1"}, by: signatory.Approver{UserID: "lic3"}, wantSigner: "clerk"},
		{name: "clerk without bond", change: signatory.Change{Add: []signatory.Signatory{{UserID: "clerk", Kind: signatory.BondedNonLicensee}}}, proposeErr: signatory.ErrNotEligible},
		{name: "clerk as licensee", change: signatory.Change{Add: []signatory.Signatory{{UserID: "clerk", Kind: signatory.Licensee}}}, proposeErr: signatory.ErrNotEligible},
		{name: "former staff", change: signatory.Change{Add: []signatory.Signatory{{UserID: "gone", Kind: signatory.Licensee}}}, proposeErr: signatory.ErrNotEligible},
		{name: "unknown user", change: signatory.Change{Add: []signatory.Signatory{{UserID: "nobody", Kind: signatory.Licensee}}}, proposeErr: signatory.ErrNotFound},
		{name: "remove last", change: signatory.Change{Remove: []string{"lic1"}}, proposeErr: signatory.ErrLastSignatory},
		{name: "preparer approves", change: signatory.Change{Add: []signatory.Signatory{{UserID: "lic2", Kind: signatory.Licensee}}, PreparedBy: "lic1"}, by: signatory.Approver{UserID: "lic1"}, approveErr: signatory.ErrSelfApproval},
		{name: "agent approves", change: signatory.Change{Add: []signatory.Signatory{{UserID: "lic2", Kind: signatory.Licensee}}, PreparedBy: "lic1"}, by: signatory.Approver{UserID: "lic3", IsAgent: true}, approveErr: signatory.ErrAgentApproval},
		{name: "non-licensee approves", change: signatory.Change{Add: []signatory.Signatory{{UserID: "lic2", Kind: signatory.Licensee}}, PreparedBy: "lic1"}, by: signatory.Approver{UserID: "clerk"}, approveErr: signatory.ErrNotEligible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := seeded(t)
			tt.change.AccountID = "a1"
			c, err := f.Propose(ctx, tt.change)
			if !errors.Is(err, tt.proposeErr) {
				t.Fatalf("propose err = %v, want %v", err, tt.proposeErr)
			}
			if err != nil {
				return
			}
			if ok, _ := f.CanSign(ctx, "a1", tt.wantSigner); ok && tt.wantSigner != "" {
				t.Fatal("pending change already applied")
			}
			if _, err := f.Approve(ctx, c.ID, tt.by); !errors.Is(err, tt.approveErr) {
				t.Fatalf("approve err = %v, want %v", err, tt.approveErr)
			}
			if tt.wantSigner != "" {
				if ok, _ := f.CanSign(ctx, "a1", tt.wantSigner); !ok {
					t.Fatalf("%s cannot sign after approval", tt.wantSigner)
				}
				if _, err := f.Approve(ctx, c.ID, tt.by); !errors.Is(err, signatory.ErrInvalidState) {
					t.Fatalf("second approve err = %v", err)
				}
			}
		})
	}
}

func TestFreezeAndRemove(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		op       func(f *Fake) error
		wantLic1 bool
		wantList int
	}{
		{name: "freeze on leaving", op: func(f *Fake) error {
			n, err := f.Freeze(ctx, "lic1")
			if n != 1 {
				t.Fatalf("frozen = %d, want 1", n)
			}
			return err
		}, wantLic1: false, wantList: 1},
		{name: "remove after adding another", op: func(f *Fake) error {
			c, err := f.Propose(ctx, signatory.Change{AccountID: "a1", Add: []signatory.Signatory{{UserID: "lic2", Kind: signatory.Licensee}}, Remove: []string{"lic1"}, PreparedBy: "lic3"})
			if err != nil {
				return err
			}
			_, err = f.Approve(ctx, c.ID, signatory.Approver{UserID: "lic2"})
			return err
		}, wantLic1: false, wantList: 1},
		{name: "freeze unknown is no-op", op: func(f *Fake) error { _, err := f.Freeze(ctx, "nobody"); return err }, wantLic1: true, wantList: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := seeded(t)
			if err := tt.op(f); err != nil {
				t.Fatal(err)
			}
			if ok, _ := f.CanSign(ctx, "a1", "lic1"); ok != tt.wantLic1 {
				t.Fatalf("lic1 CanSign = %v, want %v", ok, tt.wantLic1)
			}
			got, err := f.List(ctx, "a1")
			if err != nil || len(got) != tt.wantList {
				t.Fatalf("List = %d, %v; want %d", len(got), err, tt.wantList)
			}
		})
	}
}
