package boot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/api/apifake"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

var errFake = errors.New("fake: not stored")

type allowAll struct{}

func (allowAll) Can(context.Context, registry.Principal, string, registry.Subject) (bool, error) {
	return true, nil
}

type nilTx struct{}

func (nilTx) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, nil)
}

type noGates struct{}

func (noGates) Open(context.Context, pgx.Tx, string, gate.Subject, json.RawMessage, gate.Principal) (gate.GateID, error) {
	return "", errFake
}
func (noGates) Get(context.Context, gate.GateID) (gate.Gate, error) { return gate.Gate{}, errFake }

type noEmit struct{}

func (noEmit) Emit(context.Context, pgx.Tx, registry.Subject, string, any) error { return nil }

type noDrafts struct{}

func (noDrafts) Create(context.Context, pgx.Tx, bill.DraftRequest) (bill.ID, error) {
	return "", errFake
}
func (noDrafts) Edit(context.Context, pgx.Tx, bill.ID, bill.DraftEdit) error { return errFake }

type noCredit struct{}

func (noCredit) Entries(context.Context, credit.ListFilter) ([]credit.Entry, error) { return nil, nil }
func (noCredit) Entry(context.Context, string) (credit.Entry, error) {
	return credit.Entry{}, credit.ErrNotFound
}
func (noCredit) Bill(context.Context, string) (credit.Bill, error) {
	return credit.Bill{}, credit.ErrNotFound
}

type noTrust struct{}

func (noTrust) ListAccounts(context.Context) ([]trust.Account, error) { return nil, nil }
func (noTrust) ListLedgers(context.Context, trust.LedgerFilter) ([]trust.Ledger, error) {
	return nil, nil
}
func (noTrust) Balance(context.Context, trust.LedgerID) (trust.Balance, error) {
	return trust.Balance{}, errFake
}
func (noTrust) Txns(context.Context, trust.LedgerID) ([]trust.Txn, error) { return nil, nil }

type noWithdrawals struct{}

func (noWithdrawals) List(context.Context, withdraw.ListFilter) ([]withdraw.Listed, error) {
	return nil, nil
}
func (noWithdrawals) Listed(context.Context, string) (withdraw.Listed, error) {
	return withdraw.Listed{}, withdraw.ErrNotFound
}

type noRecons struct{}

func (noRecons) List(context.Context, recon.ListFilter) ([]recon.Reconciliation, error) {
	return nil, nil
}

type noGateList struct{}

func (noGateList) List(context.Context, gates.ListFilter) ([]gate.Gate, error) { return nil, nil }

type noExpenses struct{}

func (noExpenses) Record(context.Context, pgx.Tx, timekeeping.Expense, timekeeping.Actor) (timekeeping.ExpenseID, error) {
	return "", errFake
}
func (noExpenses) Get(context.Context, timekeeping.ExpenseID) (timekeeping.Expense, error) {
	return timekeeping.Expense{}, timekeeping.ErrNotFound
}
func (noExpenses) List(context.Context, timekeeping.ExpenseFilter) ([]timekeeping.Expense, error) {
	return nil, nil
}

func testHTTPPorts() *HTTPPorts {
	return &HTTPPorts{
		Access:   allowAll{},
		Cursors:  &apifake.Fake{},
		Problems: &server.Problems{},
		Principal: func(*http.Request) (registry.Principal, bool) {
			return registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-0000000000a1"}, true
		},
		Idem:            idemfake.New(),
		DB:              nilTx{},
		Gates:           noGates{},
		Audit:           auditfake.New(),
		Outbox:          noEmit{},
		Bills:           billfake.New(time.Now),
		Drafts:          noDrafts{},
		CreditReads:     noCredit{},
		CreditBills:     noCredit{},
		TrustLists:      noTrust{},
		TrustLedgers:    noTrust{},
		Withdrawals:     noWithdrawals{},
		Reconciliations: noRecons{},
		GateList:        noGateList{},
		Expenses:        noExpenses{},
	}
}

// served mounts every recorded Routes call on one router, the way the kernel server does.
func served(t *testing.T, calls []registry.Call) (chi.Router, map[string]int) {
	t.Helper()
	r := chi.NewRouter()
	seen := map[string]int{}
	for _, c := range calls {
		if c.Method != registry.MethodRoutes {
			continue
		}
		fn := c.Args[0].(func(chi.Router))
		for _, k := range strings.Split(routeKey(fn), "\n") {
			if k != "" {
				seen[k]++
			}
		}
		fn(r)
	}
	return r, seen
}

func TestWiredRoutesAreServed(t *testing.T) {
	rec := &registry.Recorder{}
	k := &kboot.Kernel{Registry: recorderBooter{rec}}
	if err := App(Options{HTTP: testHTTPPorts()}).Register(context.Background(), k); err != nil {
		t.Fatalf("Register: %v", err)
	}
	router, seen := served(t, rec.Calls())
	for route, n := range seen {
		if n > 1 {
			t.Errorf("%s mounted %d times; the placeholder was not replaced", route, n)
		}
	}
	const id = "00000000-0000-0000-0000-000000000001"
	tests := []struct{ method, path string }{
		{"GET", "/bills"},
		{"GET", "/bills/" + id},
		{"GET", "/bills/" + id + "/lines"},
		{"POST", "/bills"},
		{"PATCH", "/bills/" + id},
		{"POST", "/bills/" + id + "/submit"},
		{"POST", "/bills/" + id + "/void"},
		{"POST", "/bills/" + id + "/deliver"},
		{"GET", "/credit-notes"},
		{"GET", "/trust/accounts"},
		{"GET", "/trust/ledgers"},
		{"POST", "/trust/receipts"},
		{"GET", "/trust/withdrawals"},
		{"GET", "/trust/withdrawals/" + id},
		{"GET", "/trust/reconciliations"},
		{"GET", "/gates"},
		{"GET", "/gates/" + id},
		{"GET", "/expenses"},
		{"POST", "/expenses"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			if !seenPath(seen, tt.method, tt.path) {
				t.Fatalf("route not mounted")
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "k-1")
			router.ServeHTTP(w, req)
			if w.Code == http.StatusServiceUnavailable {
				t.Fatalf("status 503: route still unwired: %s", w.Body.String())
			}
		})
	}
}

// seenPath reports whether a mounted route pattern matches method and path.
func seenPath(seen map[string]int, method, path string) bool {
	for k := range seen {
		m, pattern, _ := strings.Cut(k, " ")
		if m != method {
			continue
		}
		ps, ss := strings.Split(pattern, "/"), strings.Split(path, "/")
		if len(ps) != len(ss) {
			continue
		}
		ok := true
		for i := range ps {
			if !strings.HasPrefix(ps[i], "{") && ps[i] != ss[i] {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func TestWiredRoutesNeedSharedPorts(t *testing.T) {
	tests := []struct {
		name string
		edit func(*HTTPPorts)
	}{
		{"nil Access", func(p *HTTPPorts) { p.Access = nil }},
		{"nil Cursors", func(p *HTTPPorts) { p.Cursors = nil }},
		{"nil Problems", func(p *HTTPPorts) { p.Problems = nil }},
		{"nil Principal", func(p *HTTPPorts) { p.Principal = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testHTTPPorts()
			tt.edit(p)
			k := &kboot.Kernel{Registry: recorderBooter{&registry.Recorder{}}}
			err := App(Options{HTTP: p}).Register(context.Background(), k)
			if !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("err = %v, want ErrMissingDependency", err)
			}
		})
	}
}

func TestPlaceholdersKeptWithoutHTTPPorts(t *testing.T) {
	rec := &registry.Recorder{}
	k := &kboot.Kernel{Registry: recorderBooter{rec}}
	if err := App(Options{}).Register(context.Background(), k); err != nil {
		t.Fatalf("Register: %v", err)
	}
	router, _ := served(t, rec.Calls())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/bills", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 from the unwired placeholder", w.Code)
	}
}

// On Postgres the bill routes serve with no caller-supplied Bills, Drafts or Outbox: boot
// fills them with bill.PGService, draft.PGDrafts and the kernel emitter.
func TestBillRoutesServeOnPostgres(t *testing.T) {
	p := testHTTPPorts()
	p.Bills, p.Drafts, p.Outbox = nil, nil, nil
	rec := &registry.Recorder{}
	k := testKernel()
	k.Registry, k.Emitter = recorderBooter{rec}, outboxfake.New()
	if err := App(Options{HTTP: p}).Register(context.Background(), k); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, seen := served(t, rec.Calls())
	const id = "00000000-0000-0000-0000-000000000001"
	for _, tt := range []struct{ method, path string }{
		{"GET", "/bills"}, {"POST", "/bills"}, {"PATCH", "/bills/" + id}, {"POST", "/bills/" + id + "/submit"},
		{"POST", "/bills/" + id + "/deliver"},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			if !seenPath(seen, tt.method, tt.path) {
				t.Fatalf("route not wired from Postgres")
			}
		})
	}
	got := p.withPostgresBilling(k)
	if _, ok := got.Bills.(*bill.PGService); !ok {
		t.Fatalf("Bills = %T, want *bill.PGService", got.Bills)
	}
	if _, ok := got.Drafts.(draft.PGDrafts); !ok {
		t.Fatalf("Drafts = %T, want draft.PGDrafts", got.Drafts)
	}
	if got.Outbox == nil {
		t.Fatal("Outbox not the kernel emitter")
	}
	if kept := testHTTPPorts().withPostgresBilling(k); kept.Drafts != (noDrafts{}) {
		t.Fatalf("caller Drafts replaced by %T", kept.Drafts)
	}
}
