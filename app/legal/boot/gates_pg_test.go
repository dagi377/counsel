package boot

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// capAccess grants capability cap to the users in holders.
type capAccess struct {
	cap     string
	holders map[string]bool
	err     error
}

func (a capAccess) Can(_ context.Context, p registry.Principal, c string, _ registry.Subject) (bool, error) {
	return a.holders[p.UserID] && c == a.cap, a.err
}

// rule: BIL-C-085
func TestBIL_C_085_ManagingLawyerFromCapability(t *testing.T) {
	access := capAccess{cap: CapWriteOffSecondApprove, holders: map[string]bool{"ml": true}}
	tests := []struct {
		name   string
		access Access
		user   string
		want   bool
	}{
		{"holder of the second-approval capability", access, "ml", true},
		{"other user", access, "lawyer", false},
		{"empty user", access, "", false},
		{"lookup error fails closed", capAccess{cap: CapWriteOffSecondApprove, holders: map[string]bool{"ml": true}, err: errors.New("down")}, "ml", false},
		{"no access fails closed", nil, "ml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := managingLawyer(tt.access)(tt.user); got != tt.want {
				t.Fatalf("managingLawyer(%q) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

// rule: BIL-I-001
func TestBIL_I_001_PGGLRefusesBeforeTheDatabase(t *testing.T) {
	line := func(side ledger.Side, c ledger.Cents) ledger.Line {
		return ledger.Line{Account: "OP-1100", Side: side, Amount: c}
	}
	tests := []struct {
		name string
		e    ledger.Entry
		want error
	}{
		{"unbalanced", ledger.Entry{Book: "fees", SourceKind: "bill", SourceID: "b1",
			Lines: []ledger.Line{line(ledger.Debit, 100), line(ledger.Credit, 99)}}, ledger.ErrUnbalanced},
		{"one line", ledger.Entry{Book: "fees", SourceKind: "bill", SourceID: "b1",
			Lines: []ledger.Line{line(ledger.Debit, 100)}}, ledger.ErrInvalidEntry},
		{"negative cents", ledger.Entry{Book: "fees", SourceKind: "bill", SourceID: "b1",
			Lines: []ledger.Line{line(ledger.Debit, -1), line(ledger.Credit, -1)}}, ledger.ErrInvalidEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A nil tx: any statement would panic, so the refusal happens before the database.
			if _, err := (PGGL{}).Post(context.Background(), nil, tt.e); !errors.Is(err, tt.want) {
				t.Fatalf("Post = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := (PGGL{}).Balance(context.Background(), "OP-1100", ledger.Date{}); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Balance without querier = %v", err)
	}
}

func TestGatePortsPGDependencies(t *testing.T) {
	key := trust.SealKey{ID: 1, Key: make([]byte, 32)}
	access := capAccess{}
	var log audit.Log = audit.New(noConnPool{dbfake.New()}, AuditMasker(), audit.Options{})
	tests := []struct {
		name   string
		mutate func(k **kboot.Kernel, key *trust.SealKey, log *audit.Log, a *Access)
		want   error
	}{
		{"complete", func(**kboot.Kernel, *trust.SealKey, *audit.Log, *Access) {}, nil},
		{"short seal key", func(_ **kboot.Kernel, k *trust.SealKey, _ *audit.Log, _ *Access) { k.Key = k.Key[:16] }, trust.ErrSealKey},
		{"no audit log", func(_ **kboot.Kernel, _ *trust.SealKey, l *audit.Log, _ *Access) { *l = nil }, ErrMissingDependency},
		{"no access", func(_ **kboot.Kernel, _ *trust.SealKey, _ *audit.Log, a *Access) { *a = nil }, ErrMissingDependency},
		{"no gate engine", func(k **kboot.Kernel, _ *trust.SealKey, _ *audit.Log, _ *Access) { (*k).Gates = nil }, ErrMissingDependency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := testKernel()
			k.DB = noConnPool{dbfake.New()}
			k.Gates = gatefake.New(k.Clock.Now, gatefake.Caps{})
			kk, kv, l, a := k, key, log, Access(access)
			kv.Key = slices.Clone(key.Key)
			tt.mutate(&kk, &kv, &l, &a)
			p, err := gatePortsPG(kk, noConnPool{dbfake.New()}, kv, "", l, a)
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("gatePortsPG = %v, want %v", err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("gatePortsPG: %v", err)
			}
			if p.Billing.Prefix != DefaultBillPrefix {
				t.Fatalf("prefix = %q", p.Billing.Prefix)
			}
		})
	}
}

// TestGatePortsPGMatchFakes is the contract check: the Postgres GatePorts build the same
// gate effect table as the fake ports, so every kind keeps its effect in production.
func TestGatePortsPGMatchFakes(t *testing.T) {
	k := testKernel()
	k.DB = noConnPool{dbfake.New()}
	k.Gates = gatefake.New(k.Clock.Now, gatefake.Caps{})
	p, err := gatePortsPG(k, noConnPool{dbfake.New()}, trust.SealKey{ID: 1, Key: make([]byte, 32)}, "INV",
		audit.New(noConnPool{dbfake.New()}, AuditMasker(), audit.Options{}), capAccess{})
	if err != nil {
		t.Fatalf("gatePortsPG: %v", err)
	}
	s, err := buildServices(k, *p)
	if err != nil {
		t.Fatalf("buildServices: %v", err)
	}
	fx, err := effects(k, s)
	if err != nil {
		t.Fatalf("effects: %v", err)
	}
	got := slices.Sorted(maps.Keys(fx))
	want := slices.Sorted(maps.Keys(testEffects(t)))
	if !slices.Equal(got, want) {
		t.Fatalf("pg effects = %v, fake effects = %v", got, want)
	}
}
