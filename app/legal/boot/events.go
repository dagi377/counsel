package boot

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/deadlines"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/roles"
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
	// Contacts publishes party.organization_client_created (EVENTS-WIRE-2).
	Contacts *party.Store
	// Walls publishes roles.walled_matter_accessed on each check of a walled matter.
	Walls *roles.WallResolver
	// OwnTx is the emitter for write points with no transaction of their own (the wall
	// check, intake.Leads publishing identity.portal_submission).
	OwnTx outbox.Emitter
	// Matters publishes matter.opened, matter.stage_changed and matter.note_added; its
	// OpeningEffect applies the matter.opening gate (EVENTS-WIRE-4).
	Matters *matter.Store
	// Revisions, CourtForms, Inbound and Calendar publish document.revision_requested,
	// court_form.changed, email.received and calendar_event.added (Q-EVENTS-WIRE-4).
	Revisions  *legaldocs.Revisions
	CourtForms *legaldocs.CourtForms
	Inbound    *comms.Inbound
	Calendar   *deadlines.CalendarEvents
}

// newEventStores builds the deadline book (rulepack.published,
// deadline.trigger_date_changed), the filing store (filing.receipt_received, claim.issued,
// service.affidavit_received), the email filing store (email.filed), the contact store
// (party.organization_client_created) and the wall resolver (roles.walled_matter_accessed),
// each emitting through em.
func newEventStores(pool db.DB, em outbox.Emitter) EventStores {
	book := deadline.NewPGBook(pool)
	book.Events = em
	filings := comms.NewFilingStore(pool)
	filings.Events = em
	contacts := party.NewStore(pool)
	contacts.Events = em
	own := ownTxEmitter{db: pool, em: em}
	matters := matter.NewStore(pool, contacts)
	matters.Events = em
	return EventStores{
		Matters:    matters,
		Revisions:  &legaldocs.Revisions{DB: pool, Events: em},
		CourtForms: &legaldocs.CourtForms{DB: pool, Events: em},
		Inbound:    &comms.Inbound{DB: pool, Events: em},
		Calendar:   &deadlines.CalendarEvents{DB: pool, Events: em},
		Book:       book, Filings: filings, Emails: comms.NewEmailFilingStore(pool, em),
		Contacts: contacts, OwnTx: own,
		Walls: roles.NewWallResolver(matter.PGWallStore{DB: pool}, nil).WithEvents(own),
	}
}

// ownTxEmitter emits in the caller's tx, or in a transaction of its own when the caller has
// none (tx nil). The kernel emitter needs a tx; a read-side publisher has none to give.
type ownTxEmitter struct {
	db db.DB
	em outbox.Emitter
}

// Emit implements outbox.Emitter.
func (o ownTxEmitter) Emit(ctx context.Context, tx pgx.Tx, s outbox.Subject, typ string, body any) error {
	if tx != nil || o.db == nil {
		return o.em.Emit(ctx, tx, s, typ, body)
	}
	return o.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return o.em.Emit(ctx, tx, s, typ, body)
	})
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
