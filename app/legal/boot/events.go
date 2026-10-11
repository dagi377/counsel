package boot

import (
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/outbox"
)

// Event emitters: every module write point that publishes an event gets the kernel emitter
// here, so one file shows which modules publish and none is left with a nil emitter (a nil
// emitter makes those write points refuse or stay silent).

// EventStores are the publishing stores boot builds on the kernel DB and emitter.
type EventStores struct {
	Book    *deadline.PGBook
	Filings *comms.FilingStore
	Emails  *comms.EmailFilingStore
}

// newEventStores builds the deadline book (rulepack.published,
// deadline.trigger_date_changed), the filing store (filing.receipt_received, claim.issued,
// service.affidavit_received) and the email filing store (email.filed), each emitting
// through em.
func newEventStores(pool db.DB, em outbox.Emitter) EventStores {
	book := deadline.NewPGBook(pool)
	book.Events = em
	filings := comms.NewFilingStore(pool)
	filings.Events = em
	return EventStores{Book: book, Filings: filings, Emails: comms.NewEmailFilingStore(pool, em)}
}

// SetMatterPortEvents makes the matter-side agent ports that publish emit through em:
// PGConflictParties publishes matter.party_added. Call it after WireMatterPorts.
func SetMatterPortEvents(p *AgentPorts, em outbox.Emitter) {
	if p == nil {
		return
	}
	if cp, ok := p.ConflictParties.(PGConflictParties); ok {
		cp.Events = em
		p.ConflictParties = cp
	}
}

// setCheckEvents makes checks publish conflict.hits_disposed through em (CON-03-MEMO).
func setCheckEvents(checks *conflict.Checks, em outbox.Emitter) {
	if checks != nil {
		checks.Events = em
	}
}
