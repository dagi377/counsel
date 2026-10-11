package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolGetBill reads one bill: its lines, taxes, totals and balance, in cents.
const ToolGetBill = "get_bill"

// BillReader reads one bill (app/legal/billing/bill Service.Get).
type BillReader interface {
	Get(ctx context.Context, id bill.ID) (bill.Bill, error)
}

// BillBalances returns what is still owed on a bill, in cents: its total less payments,
// credits and write-offs applied to it. Wired at boot from billing.
type BillBalances interface {
	BalanceCents(ctx context.Context, id bill.ID) (int64, error)
}

// GetBillHandler is get_bill's handler, for the boot HandlerSet.
func GetBillHandler() Handler {
	return Handler{Name: ToolGetBill, Build: buildGetBill}
}

// buildGetBill reads Deps: CapabilityCheck, roles.Walls, BillReader, BillBalances and the
// run's *agenttools.Provenance.
func buildGetBill(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	bills, err := DepOf[BillReader](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	balances, err := DepOf[BillBalances](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	g := getBill{walls: walls, bills: bills, balances: balances}
	t, err := handlerTool(ToolGetBill, Read, can, g.handle)
	if err != nil {
		return registry.Tool{}, err
	}
	// The cents this tool returns are the figures a later write in the run may carry.
	t, err = pv.ReadTool(t)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	return t, nil
}

type getBill struct {
	walls    roles.Walls
	bills    BillReader
	balances BillBalances
}

// billLineOut is one bill line: numbers and codes only. Its narrative is lawyer text about
// client work and travels as a quote, cited to the line.
type billLineOut struct {
	Ordinal       int    `json:"ordinal"`
	Source        string `json:"source"`
	QuantityMilli int64  `json:"quantity_milli"`
	RateCents     int64  `json:"rate_cents"`
	AmountCents   int64  `json:"amount_cents"`
	TaxCode       string `json:"tax_code,omitempty"`
}

type getBillOut struct {
	BillID              string              `json:"bill_id"`
	Number              string              `json:"number,omitempty"`
	MatterID            string              `json:"matter_id"`
	Status              string              `json:"status"`
	IssueDate           string              `json:"issue_date,omitempty"`
	DueDate             string              `json:"due_date,omitempty"`
	Lines               []billLineOut       `json:"lines"`
	FeesCents           int64               `json:"fees_cents"`
	TaxableDisbCents    int64               `json:"taxable_disbursements_cents"`
	NontaxableDisbCents int64               `json:"nontaxable_disbursements_cents"`
	DiscountCents       int64               `json:"discount_cents"`
	TaxCents            int64               `json:"tax_cents"`
	TotalCents          int64               `json:"total_cents"`
	BalanceCents        int64               `json:"balance_cents"`
	Quotes              []agenttools.Quoted `json:"quotes"`
}

// BillLineCitation cites one line of a bill. A bill has no document version, so the bill id
// stands in for the version, at page 1, line = the line's ordinal.
func BillLineCitation(id bill.ID, ordinal int) agenttools.Citation {
	return agenttools.Citation{VersionID: "bill:" + string(id), Page: 1, FromLine: ordinal, ToLine: ordinal}
}

// civilDate is t as a date, or "" when t is zero. Bill dates are already business dates in
// America/Toronto, so only the calendar fields are read.
func civilDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

func (g getBill) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		BillID string `json:"bill_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.BillID == "" {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetBill, ErrInvalidInput)
	}
	id := bill.ID(in.BillID)
	b, err := g.bills.Get(ctx, id)
	// rule: ARC-I-036
	// An unknown bill and a walled one look the same: not found.
	if errors.Is(err, bill.ErrNotFound) {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetBill, ErrNotVisible)
	}
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: bill: %w", ToolGetBill, err)
	}
	if err := visibleTo(ctx, g.walls, user, b.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	balance, err := g.balances.BalanceCents(ctx, id)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: balance: %w", ToolGetBill, err)
	}
	out := getBillOut{
		BillID: string(b.ID), Number: b.Number, MatterID: b.MatterID, Status: string(b.Status),
		IssueDate: civilDate(b.IssueDate), DueDate: civilDate(b.DueDate),
		Lines:     make([]billLineOut, 0, len(b.Lines)),
		FeesCents: b.FeesCents, TaxableDisbCents: b.TaxableDisbCents, NontaxableDisbCents: b.NontaxableDisbCents,
		DiscountCents: b.DiscountCents, TaxCents: b.TaxCents, TotalCents: b.TotalCents, BalanceCents: balance,
		Quotes: []agenttools.Quoted{},
	}
	var cites []agenttools.Citation
	for _, l := range b.Lines {
		out.Lines = append(out.Lines, billLineOut{
			Ordinal: l.Ordinal, Source: string(l.Source), QuantityMilli: l.QuantityMilli,
			RateCents: l.RateCents, AmountCents: l.AmountCents, TaxCode: l.TaxCode,
		})
		if l.Narrative == "" {
			continue
		}
		// rule: ARC-I-038
		// A narrative is text about client work: it goes back quoted as untrusted data.
		src := BillLineCitation(b.ID, max(l.Ordinal, 1))
		out.Quotes = append(out.Quotes, agenttools.Quote(src, l.Narrative))
		cites = append(cites, src)
	}
	res, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetBill, err)
	}
	return registry.ToolResult{Output: res, Citations: cites}, nil
}
