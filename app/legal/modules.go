package legal

import (
	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/assessment"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/contingency"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/feeshare"
	"github.com/dagi377/counsel/app/legal/billing/interest"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/split"
	"github.com/dagi377/counsel/app/legal/billing/surcharge"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/billing/total"
	"github.com/dagi377/counsel/app/legal/billing/trustapply"
	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/clio"
	cliosync "github.com/dagi377/counsel/app/legal/clio/sync"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/export"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/app/legal/limitation"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/reports"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/rates"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/registry"
)

// module is one app/legal package and its Register function.
type module struct {
	name     string
	register func(registry.Registry) error
}

// modules lists every app/legal package that registers with the kernel, in boot order:
// roles and parties first, then the modules that refer to them. A new package adds one line here;
// a new feature inside a package adds a file that appends to that package's registrars.
var modules = []module{
	{"roles", roles.Register},
	{"access", access.Register},
	{"party", party.Register},
	{"matter", matter.Register},
	{"conflict", conflict.Register},
	{"intake", intake.Register},
	{"limitation", limitation.Register},
	{"rulepacks/ontario", ontario.Register},
	{"timekeeping", timekeeping.Register},
	{"timekeeping/rates", rates.Register},
	{"books", books.Register},
	{"trust", trust.Register},
	{"trust/signatory", signatory.Register},
	{"trust/withdraw", withdraw.Register},
	{"trust/recon", recon.Register},
	{"billing/draft", draft.Register},
	{"billing/split", split.Register},
	{"billing/tax", tax.Register},
	{"billing/surcharge", surcharge.Register},
	{"billing/interest", interest.Register},
	{"billing/contingency", contingency.Register},
	{"billing/feeshare", feeshare.Register},
	{"billing/total", total.Register},
	{"billing/bill", bill.Register},
	{"billing/batch", batch.Register},
	{"billing/credit", credit.Register},
	{"billing/payment", payment.Register},
	{"billing/trustapply", trustapply.Register},
	{"billing/assessment", assessment.Register},
	{"gates", gates.Register},
	{"docs", docs.Register},
	{"comms", comms.Register},
	{"reports", reports.Register},
	{"export", export.Register},
	{"clio", clio.Register},
	{"clio/sync", cliosync.Register},
	{"agent", agent.Register},
}
