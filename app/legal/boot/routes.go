package boot

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/deadlines"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Access is identity.Access as the route handlers use it. Every module's Access port has this
// one method, so one value serves them all.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTPPorts are the route handlers' dependencies. The shared ones (Access, Cursors,
// Problems, Principal) are required for any route to be wired. Each route group is wired only
// when its own ports are set too; a group left out keeps the module's placeholder mount, which
// answers 503. DB defaults to the kernel DB and Gates to the kernel gate engine.
type HTTPPorts struct {
	Access    Access
	Cursors   api.CursorCodec
	Problems  server.ProblemWriter
	Principal func(r *http.Request) (registry.Principal, bool)
	Idem      idem.Store
	DB        TxRunner
	Gates     GateOpener
	Audit     audit.Log
	Outbox    bill.Emitter

	// Bills serves GET /bills*; with Drafts it also serves POST/PATCH /bills, and with
	// Outbox and Gates the submit, void and deliver actions.
	Bills  bill.Service
	Drafts bill.Drafts
	// CreditReads and CreditBills serve /credit-notes (HTTP-09).
	CreditReads credit.Reads
	CreditBills credit.Bills
	// TrustLists and TrustLedgers serve /trust/accounts and /trust/ledgers (HTTP-10).
	TrustLists   trust.Lists
	TrustLedgers trust.LedgerReads
	// Withdrawals serves /trust/withdrawals and Reconciliations /trust/reconciliations
	// (HTTP-11).
	Withdrawals     withdraw.Lists
	Reconciliations recon.Lists
	// GateList serves GET /gates over core.gate under the caller's RLS.
	GateList gates.Lister
	// Expenses serves /expenses (HTTP-05). Nil uses the Postgres ExpenseStore on the kernel DB.
	Expenses timekeeping.Expenses
	// Events serves the trigger change, claim issue, affidavit of service and email filing
	// routes (EVENTS-WIRE-2). Nil builds them on the kernel DB and emitter.
	Events *EventStores
}

// TxRunner runs fn in one transaction; kernel/db implements it.
type TxRunner = bill.TxRunner

// GateOpener is the part of the kernel gate engine the write routes use.
type GateOpener = bill.Gates

// mount is one wired route group: the handler's Routes func.
type mount func(chi.Router)

// wiredRoutes builds every route group whose ports are set. The gate inbox and the
// approver counts read the spec gate catalog.
func wiredRoutes(k *kboot.Kernel, p HTTPPorts) ([]mount, error) {
	specs, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		return nil, fmt.Errorf("http ports: gate catalog: %w", err)
	}
	cat, approvers := gates.NewCatalog(specs), minApprovers(specs)
	if p.Access == nil || p.Cursors == nil || p.Problems == nil || p.Principal == nil {
		return nil, fmt.Errorf("http ports: Access, Cursors, Problems and Principal: %w", ErrMissingDependency)
	}
	if p.DB == nil && k.DB != nil {
		p.DB = k.DB
	}
	if p.Gates == nil && k.Gates != nil {
		p.Gates = k.Gates
	}
	if p.Expenses == nil && k.DB != nil && k.Clock != nil {
		p.Expenses = timekeeping.NewExpenseStore(k.DB, k.Clock.Location())
	}
	if p.Events == nil && k.DB != nil && k.Emitter != nil {
		ev := newEventStores(k.DB, k.Emitter)
		p.Events = &ev
	}
	var out []mount
	add := func(ok bool, fn mount) {
		if ok {
			out = append(out, fn)
		}
	}
	add(p.Bills != nil, (&bill.HTTP{Bills: p.Bills, Access: p.Access, Cursors: p.Cursors,
		Problems: p.Problems, Principal: p.Principal}).Routes)
	add(p.Bills != nil && p.Drafts != nil && p.DB != nil && p.Idem != nil, (&bill.DraftHTTP{
		Bills: p.Bills, Drafts: p.Drafts, DB: p.DB, Access: p.Access, Idem: p.Idem,
		Problems: p.Problems, Principal: p.Principal}).Routes)
	add(p.Bills != nil && p.Gates != nil && p.Outbox != nil && p.DB != nil && p.Idem != nil, (&bill.ActionHTTP{
		Bills: p.Bills, Gates: p.Gates, Outbox: p.Outbox, DB: p.DB, Access: p.Access, Idem: p.Idem,
		Problems: p.Problems, Principal: p.Principal, MinApprovers: approvers}).Routes)
	add(p.CreditReads != nil && p.CreditBills != nil && p.DB != nil && p.Gates != nil && p.Idem != nil, (&credit.HTTP{
		Reads: p.CreditReads, Bills: p.CreditBills, Access: p.Access, DB: p.DB, Gates: p.Gates,
		Idem: p.Idem, Cursors: p.Cursors, Problems: p.Problems, Principal: p.Principal,
		MinApprovers: approvers[credit.GateKindCreditNote]}).Routes)
	add(p.TrustLists != nil && p.TrustLedgers != nil, (&trust.HTTP{Lists: p.TrustLists,
		Ledgers: p.TrustLedgers, Access: p.Access, Cursors: p.Cursors, Problems: p.Problems,
		Principal: p.Principal}).Routes)
	add(p.TrustLists != nil && p.DB != nil && p.Gates != nil && p.Idem != nil, (&trust.ReceiptHTTP{
		Lists: p.TrustLists, Journal: trust.PGReceiptJournal{}, Access: p.Access, DB: p.DB, Gates: p.Gates,
		Idem: p.Idem, Problems: p.Problems, Principal: p.Principal,
		MinApprovers: approvers[trust.GateReceiptClassification]}).Routes)
	add(p.Withdrawals != nil, (&withdraw.Reads{Lists: p.Withdrawals, Access: p.Access,
		Cursors: p.Cursors, Problems: p.Problems, Principal: p.Principal}).Routes)
	add(p.Reconciliations != nil, (&recon.Reads{Lists: p.Reconciliations, Access: p.Access,
		Cursors: p.Cursors, Problems: p.Problems, Principal: p.Principal}).Routes)
	add(p.GateList != nil && p.Gates != nil && p.Audit != nil && p.DB != nil, (&gates.HTTP{
		Gates: p.Gates, List: p.GateList, Kinds: cat, Access: p.Access, Audit: p.Audit, DB: p.DB,
		Cursors: p.Cursors, Problems: p.Problems, Principal: p.Principal}).Routes)
	add(p.Expenses != nil && p.DB != nil && p.Idem != nil, (&timekeeping.ExpenseHTTP{
		Expenses: p.Expenses, DB: p.DB, Access: p.Access, Cursors: p.Cursors, Idem: p.Idem,
		Problems: p.Problems, Principal: p.Principal}).Routes)
	if ev := p.Events; ev != nil && p.Idem != nil {
		add(ev.Book != nil && p.DB != nil, (&deadlines.TriggerHTTP{Book: ev.Book, DB: p.DB, Access: p.Access,
			Idem: p.Idem, Problems: p.Problems, Principal: p.Principal}).Routes)
		add(ev.Filings != nil && ev.Emails != nil, (&comms.FilingHTTP{Filings: ev.Filings, Emails: ev.Emails,
			Access: p.Access, Idem: p.Idem, Problems: p.Problems, Principal: p.Principal}).Routes)
	}
	return out, nil
}

// minApprovers maps each spec gate kind to its approver count.
func minApprovers(specs []gates.SpecKind) map[string]int {
	out := make(map[string]int, len(specs))
	for _, k := range specs {
		out[k.Code] = k.MinApprovers
	}
	return out
}

// routeKey is the sorted "METHOD pattern" set fn mounts, joined by newlines. Two mounts with
// one key serve the same routes.
func routeKey(fn func(chi.Router)) string {
	r := chi.NewRouter()
	fn(r)
	var keys []string
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		keys = append(keys, method+" "+route)
		return nil
	})
	slices.Sort(keys)
	return strings.Join(keys, "\n")
}

// routeFilter is the registry the legal modules register into when boot wires routes: a
// module's placeholder mount whose routes boot serves with a wired handler is dropped, so the
// kernel server never sees one route twice. Every other call passes through.
type routeFilter struct {
	registry.Registry
	wired map[string]bool
}

// Routes drops fn when boot mounts a wired handler for the same routes.
func (f routeFilter) Routes(fn func(chi.Router)) {
	if f.wired[routeKey(fn)] {
		return
	}
	f.Registry.Routes(fn)
}

// newRouteFilter returns r filtered for the wired mounts.
func newRouteFilter(r registry.Registry, wired []mount) routeFilter {
	f := routeFilter{Registry: r, wired: map[string]bool{}}
	for _, m := range wired {
		f.wired[routeKey(m)] = true
	}
	return f
}
