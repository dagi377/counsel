package boot

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/api/apifake"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// sharedPorts are the HTTP ports every route group needs; no module read port is set.
func sharedPorts() *HTTPPorts {
	return &HTTPPorts{
		Access:   allowAll{},
		Cursors:  &apifake.Fake{},
		Problems: &server.Problems{},
		Principal: func(*http.Request) (registry.Principal, bool) {
			return registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-0000000000a1"}, true
		},
		Idem:  idemfake.New(),
		Gates: noGates{},
		Audit: auditfake.New(),
	}
}

func pgKernel() *kboot.Kernel {
	return &kboot.Kernel{DB: dbfake.New(), Clock: clockfake.New(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC), time.UTC)}
}

func TestWithReadersFillsPostgresPorts(t *testing.T) {
	p := sharedPorts().withReaders(pgKernel())
	tests := []struct {
		name string
		ok   bool
	}{
		{"CreditReads", is[credit.PGReads](p.CreditReads)},
		{"TrustLists", is[trust.PGLists](p.TrustLists)},
		{"TrustLedgers", is[*trust.LedgerStore](p.TrustLedgers)},
		{"Withdrawals", is[withdraw.PGLists](p.Withdrawals)},
		{"Reconciliations", is[recon.PGLists](p.Reconciliations)},
		{"GateList", is[gates.PGLister](p.GateList)},
		{"WithdrawLedgers", is[withdraw.PGLedgers](p.WithdrawLedgers)},
		{"WithdrawCancels", is[withdraw.PGCancels](p.WithdrawCancels)},
		{"ReconVersions", is[recon.PGVersions](p.ReconVersions)},
		{"ReconParser", is[recon.NoParser](p.ReconParser)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s is not the Postgres implementation", tt.name)
			}
		})
	}
}

func TestWithReadersKeepsGivenPorts(t *testing.T) {
	p := testHTTPPorts().withReaders(pgKernel())
	if _, ok := p.TrustLists.(noTrust); !ok {
		t.Fatalf("TrustLists = %T, want the caller's port kept", p.TrustLists)
	}
	if _, ok := p.CreditReads.(noCredit); !ok {
		t.Fatalf("CreditReads = %T, want the caller's port kept", p.CreditReads)
	}
}

func TestWithReadersNeedsKernelDB(t *testing.T) {
	p := sharedPorts().withReaders(&kboot.Kernel{})
	if p.TrustLists != nil || p.GateList != nil || p.ReconParser != nil {
		t.Fatalf("ports filled without a kernel DB")
	}
}

// TestReadRoutesServeOnPostgres: with only the shared ports and a kernel DB, boot wires the
// read routes over the Postgres readers, so they replace the 503 placeholders.
func TestReadRoutesServeOnPostgres(t *testing.T) {
	wired, err := wiredRoutes(pgKernel(), *sharedPorts())
	if err != nil {
		t.Fatalf("wiredRoutes: %v", err)
	}
	seen := map[string]int{}
	for _, m := range wired {
		for _, k := range strings.Split(routeKey(m), "\n") {
			seen[k]++
		}
	}
	for _, route := range []string{
		"GET /trust/accounts",
		"GET /trust/ledgers",
		"GET /trust/ledgers/{ledger_id}/lines",
		"GET /trust/withdrawals",
		"GET /trust/withdrawals/{withdrawal_id}",
		"GET /trust/reconciliations",
		"GET /gates",
		"GET /gates/{gate_id}",
	} {
		t.Run(route, func(t *testing.T) {
			if seen[route] != 1 {
				t.Fatalf("%s wired %d times, want 1", route, seen[route])
			}
		})
	}
}

func TestRegisterServesReadRoutesOnPostgres(t *testing.T) {
	rec := &registry.Recorder{}
	k := pgKernel()
	k.Registry = recorderBooter{rec}
	if err := App(Options{HTTP: sharedPorts()}).Register(context.Background(), k); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, seen := served(t, rec.Calls())
	for route, n := range seen {
		if n > 1 {
			t.Errorf("%s mounted %d times; the placeholder was not replaced", route, n)
		}
	}
}
