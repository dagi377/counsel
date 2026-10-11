package boot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/ledger"
)

// BillingPorts are the billing services' dependencies that the composition root's caller
// supplies.
type BillingPorts struct {
	// GL is the kernel ledger: the approval effect posts the bill's accrual entry on it
	// (BIL-J-003) and the void effect reverses that entry.
	GL ledger.Ledger
	// Prefix is the bill number series prefix from config, e.g. "INV".
	Prefix string
	// VoidSink is told about each void inside the gate tx (time entries freed for
	// re-billing). Optional.
	VoidSink bill.VoidedSink
}

// billingSvcs are the billing services built at boot.
type billingSvcs struct {
	Approve *bill.ApproveEffect
	Void    *bill.VoidEffect
}

// billingServices builds the bill approval and void gate effects over the billing Postgres
// stores. The approval reads its journal lines from bill.PGStore and posts on the GL; the
// void finds that entry through bill.PGStore and reverses it on the same GL.
func billingServices(k *kboot.Kernel, p BillingPorts) (billingSvcs, error) {
	if k == nil {
		return billingSvcs{}, fmt.Errorf("billing services: kernel: %w", ErrMissingDependency)
	}
	for _, d := range []struct {
		name string
		nil  bool
	}{
		{"kernel Clock", k.Clock == nil},
		{"GL", p.GL == nil},
		{"Prefix", p.Prefix == ""},
	} {
		if d.nil {
			return billingSvcs{}, fmt.Errorf("billing services: %s: %w", d.name, ErrMissingDependency)
		}
	}
	if _, err := bill.FormatNumber(p.Prefix, 2000, 1); err != nil {
		return billingSvcs{}, fmt.Errorf("billing services: Prefix: %w", err)
	}
	return billingSvcs{
		Approve: &bill.ApproveEffect{
			Store:    bill.PGStore{},
			FeesBook: bill.PGFeesBook{},
			Journal:  bill.PGStore{},
			Ledger:   p.GL,
			Prefix:   p.Prefix,
			Now:      k.Clock.Now,
		},
		Void: &bill.VoidEffect{
			Store:   bill.PGStore{},
			Entries: bill.PGStore{},
			Ledger:  p.GL,
			Sink:    p.VoidSink,
			Now:     k.Clock.Now,
		},
	}, nil
}

// ClientPlaces is the draft.Places adapter over the matter's client: the place-of-supply
// facts are the province and country of the client's first address (party.contact.addresses)
// and whether the client is an organization. A client with no province on file gives an
// empty ClientProvince, which the decider refuses (draft.ErrPlaceUnknown). Court and property
// provinces and the firm's provincial registrations are not recorded yet, so they stay empty.
type ClientPlaces struct {
	Matters MatterReader
}

var _ draft.Places = ClientPlaces{}

const clientPlaceSQL = `
SELECT coalesce(upper(btrim(addresses->0->>'province')), ''), coalesce(upper(btrim(addresses->0->>'country')), ''),
       kind = 'organization'
  FROM party.contact WHERE id = $1::uuid`

// Supply implements draft.Places. A Canadian address (country CA or none given) is taxed by
// its province; a foreign country is passed as the client's code so the non-resident rules
// apply. The client comes from the matter module's reader (ARC-I-041); a nil reader, an
// unknown matter or a matter without a client fails closed.
func (p ClientPlaces) Supply(ctx context.Context, tx pgx.Tx, matterID string) (tax.Supply, error) {
	client, err := MatterPayers(p).Payer(ctx, tx, matterID)
	if err != nil {
		return tax.Supply{}, fmt.Errorf("supply: %w", err)
	}
	var province, country string
	var business bool
	err = tx.QueryRow(ctx, clientPlaceSQL, client).Scan(&province, &country, &business)
	if errors.Is(err, pgx.ErrNoRows) {
		return tax.Supply{}, fmt.Errorf("supply of %s: client %s: %w", matterID, client, party.ErrNotFound)
	}
	if err != nil {
		return tax.Supply{}, fmt.Errorf("supply of %s: %w", matterID, err)
	}
	s := tax.Supply{ClientProvince: province, RecipientBusiness: business, ServiceType: "general"}
	switch country {
	case "CA":
		s.RecipientResident = true
	case "":
	default:
		s.ClientProvince = country
	}
	return s, nil
}

// draftWIP adapts timekeeping.PGWIP, which owns the time and expense rows, to draft.WIP.
type draftWIP struct{ wip timekeeping.PGWIP }

func (a draftWIP) Unbilled(ctx context.Context, tx pgx.Tx, matterID string, from, to time.Time) ([]draft.WIPItem, error) {
	items, err := a.wip.Unbilled(ctx, tx, matterID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]draft.WIPItem, 0, len(items))
	for _, it := range items {
		src := bill.SourceTime
		if it.Kind == "expense" {
			src = bill.SourceExpense
		}
		out = append(out, draft.WIPItem{Source: src, SourceID: it.ID, Date: it.Date, Narrative: it.Narrative,
			QuantityMilli: it.QuantityMilli, RateCents: it.RateCents, AmountCents: it.AmountCents, Agency: it.Agency})
	}
	return out, nil
}

func (a draftWIP) Claim(ctx context.Context, tx pgx.Tx, billID string, items []draft.WIPItem) error {
	in := make([]timekeeping.WIPItem, 0, len(items))
	for _, it := range items {
		kind := "time"
		if it.Source == bill.SourceExpense {
			kind = "expense"
		}
		in = append(in, timekeeping.WIPItem{Kind: kind, ID: it.SourceID})
	}
	return a.wip.Claim(ctx, tx, billID, in)
}

// MatterPayers is the matter-side draft.Payers adapter: a bill goes to the matter's client,
// read through the matter module's own reader (ARC-I-041).
type MatterPayers struct {
	Matters MatterReader
}

var _ draft.Payers = MatterPayers{}

// Payer implements draft.Payers. A nil reader or a matter without a client fails closed.
func (p MatterPayers) Payer(ctx context.Context, _ pgx.Tx, matterID string) (string, error) {
	if p.Matters == nil {
		return "", fmt.Errorf("payer of %s: matter reader: %w", matterID, ErrMissingDependency)
	}
	m, err := p.Matters.Get(ctx, matter.ID(matterID))
	if err != nil {
		return "", fmt.Errorf("payer of %s: %w", matterID, err)
	}
	if m.ClientID == "" {
		return "", fmt.Errorf("payer of %s: no client: %w", matterID, matter.ErrNotFound)
	}
	return string(m.ClientID), nil
}

// withPostgresBilling fills nil bill ports from Postgres when the kernel has a DB and clock:
// Bills is bill.PGService, Drafts is draft.PGDrafts over timekeeping's WIP and the matter's
// client, and Outbox is the kernel emitter. Ports the caller set are kept.
func (p HTTPPorts) withPostgresBilling(k *kboot.Kernel) HTTPPorts {
	if k == nil || k.DB == nil || k.Clock == nil {
		return p
	}
	if p.Bills == nil {
		p.Bills = bill.NewPGService(k.DB, k.Clock.Now)
	}
	if p.Drafts == nil {
		matters := matter.NewStore(k.DB, party.NewStore(k.DB))
		p.Drafts = draft.PGDrafts{
			WIP:     draftWIP{},
			Payers:  MatterPayers{Matters: matters},
			Places:  ClientPlaces{Matters: matters},
			Decider: tax.NewPlaceOfSupply(),
			Calc:    tax.NewCalculator(tax.NewRateTable(tax.Seed())),
			Now:     k.Clock.Now,
		}
	}
	if p.Outbox == nil && k.Emitter != nil {
		p.Outbox = k.Emitter
	}
	return p
}

// addBillDelivery adds the bill delivery outbox consumer: the kernel's outbox.relay River
// job hands it each billing.bill.delivered message, and it records the delivery through
// bill.PGService, once per message id. It needs the kernel outbox, DB and clock; without
// them it adds nothing.
func addBillDelivery(_ context.Context, k *kboot.Kernel) error {
	if k == nil || k.Outbox == nil || k.DB == nil || k.Clock == nil {
		return nil
	}
	if err := k.AddConsumer(bill.DeliverConsumer(bill.NewPGService(k.DB, k.Clock.Now))); err != nil {
		return fmt.Errorf("bill delivery consumer: %w", err)
	}
	return nil
}
