package boot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// withdrawingGates are noGates that can also withdraw, as the kernel gate engine can.
type withdrawingGates struct{ noGates }

func (withdrawingGates) Withdraw(context.Context, pgx.Tx, gate.GateID, gate.Principal) error {
	return errFake
}

// writePorts are the shared ports with gates that can withdraw; no module port is set.
func writePorts() *HTTPPorts {
	p := sharedPorts()
	p.Gates = withdrawingGates{}
	return p
}

func TestWithWritesFillsPostgresPorts(t *testing.T) {
	p := writePorts().withWrites(pgKernel())
	tests := []struct {
		name string
		ok   bool
	}{
		{"CreditBills", is[credit.PGBills](p.CreditBills)},
		{"WithdrawRequests", is[withdraw.PGService](p.WithdrawRequests)},
		{"WithdrawGates", is[withdrawingGates](p.WithdrawGates)},
		{"Recons", is[recon.PGService](p.Recons)},
		{"ReconStatements", is[recon.TxStatements](p.ReconStatements)},
		{"Payments", is[payment.PGService](p.Payments)},
		{"PaymentProposals", is[payment.PGProposals](p.PaymentProposals)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s is not the Postgres implementation", tt.name)
			}
		})
	}
}

func TestWithWritesKeepsGivenPorts(t *testing.T) {
	given := credit.PGBills{Lawyers: MatterLawyers{}}
	p := writePorts()
	p.CreditBills = given
	if got := p.withWrites(pgKernel()).CreditBills; got != given {
		t.Fatalf("CreditBills = %#v, want the caller's port kept", got)
	}
}

func TestWithWritesNeedsKernelDB(t *testing.T) {
	p := writePorts().withWrites(&kboot.Kernel{})
	if p.CreditBills != nil || p.WithdrawRequests != nil || p.Recons != nil || p.ReconStatements != nil ||
		p.Payments != nil || p.PaymentProposals != nil {
		t.Fatal("ports filled without a kernel DB")
	}
}

// TestWriteRoutesServeOnPostgres: with only the shared ports and a kernel DB, the credit-note,
// withdrawal and reconciliation POST routes and the payment routes are wired over
// Postgres and no longer answer 503.
func TestWriteRoutesServeOnPostgres(t *testing.T) {
	rec := &registry.Recorder{}
	k := pgKernel()
	k.Registry = recorderBooter{rec}
	if err := App(Options{HTTP: writePorts()}).Register(context.Background(), k); err != nil {
		t.Fatalf("Register: %v", err)
	}
	router, seen := served(t, rec.Calls())
	for route, n := range seen {
		if n > 1 {
			t.Errorf("%s mounted %d times; the placeholder was not replaced", route, n)
		}
	}
	const id = "00000000-0000-0000-0000-000000000001"
	for _, path := range []string{
		"/credit-notes",
		"/trust/withdrawals",
		"/trust/withdrawals/" + id + "/cancel",
		"/trust/reconciliations",
		"/trust/reconciliations/" + id + "/submit",
		"/payments",
		"/payments/" + id + "/allocations",
		"/payments/" + id + "/refunds",
	} {
		t.Run("POST "+path, func(t *testing.T) {
			if !seenPath(seen, http.MethodPost, path) {
				t.Fatal("route not mounted")
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "k-1")
			req.Header.Set("If-Match", `"1"`)
			router.ServeHTTP(w, req)
			if w.Code == http.StatusServiceUnavailable {
				t.Fatalf("status 503: route still unwired: %s", w.Body.String())
			}
		})
	}
}
