// Package all blank-imports every kernel package so each one can register itself
// (routes via kernel/server Mount, and the rest) from its own files at init.
// A new kernel package adds one line here.
package all

import (
	_ "github.com/dagi377/counsel/kernel/agenttools"
	_ "github.com/dagi377/counsel/kernel/api"
	_ "github.com/dagi377/counsel/kernel/audit"
	_ "github.com/dagi377/counsel/kernel/backup"
	_ "github.com/dagi377/counsel/kernel/clock"
	_ "github.com/dagi377/counsel/kernel/db"
	_ "github.com/dagi377/counsel/kernel/deadline"
	_ "github.com/dagi377/counsel/kernel/docs"
	_ "github.com/dagi377/counsel/kernel/flags"
	_ "github.com/dagi377/counsel/kernel/gate"
	_ "github.com/dagi377/counsel/kernel/idem"
	_ "github.com/dagi377/counsel/kernel/identity"
	_ "github.com/dagi377/counsel/kernel/ids"
	_ "github.com/dagi377/counsel/kernel/importer"
	_ "github.com/dagi377/counsel/kernel/inbound"
	_ "github.com/dagi377/counsel/kernel/ledger"
	_ "github.com/dagi377/counsel/kernel/metrics"
	_ "github.com/dagi377/counsel/kernel/money"
	_ "github.com/dagi377/counsel/kernel/notify"
	_ "github.com/dagi377/counsel/kernel/numseq"
	_ "github.com/dagi377/counsel/kernel/outbox"
	_ "github.com/dagi377/counsel/kernel/portal"
	_ "github.com/dagi377/counsel/kernel/report"
	_ "github.com/dagi377/counsel/kernel/retention"
	_ "github.com/dagi377/counsel/kernel/server"
	_ "github.com/dagi377/counsel/kernel/server/middleware"
	_ "github.com/dagi377/counsel/kernel/webhook"
	_ "github.com/dagi377/counsel/kernel/work"
)
