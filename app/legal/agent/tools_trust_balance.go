package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolGetTrustBalance reads the trust money available for one matter, in cents.
const ToolGetTrustBalance = "get_trust_balance"

// TrustAvailable is what may be drawn from one matter's ledger on one trust account
// (app/legal/trust/withdraw Service.Available): cleared posted money less protected, minimum
// and authorized-not-executed amounts.
type TrustAvailable interface {
	Available(ctx context.Context, trustAccountID, matterID string) (int64, error)
}

// MatterTrustAccounts lists the trust accounts that hold a ledger for a matter, in a stable
// order. Wired at boot from the trust ledgers.
type MatterTrustAccounts interface {
	TrustAccountsOf(ctx context.Context, matterID string) ([]string, error)
}

// GetTrustBalanceHandler is get_trust_balance's handler, for the boot HandlerSet.
func GetTrustBalanceHandler() Handler {
	return Handler{Name: ToolGetTrustBalance, Build: buildGetTrustBalance}
}

// buildGetTrustBalance reads Deps: CapabilityCheck, roles.Walls, TrustAvailable,
// MatterTrustAccounts and the run's *agenttools.Provenance.
func buildGetTrustBalance(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	avail, err := DepOf[TrustAvailable](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	accounts, err := DepOf[MatterTrustAccounts](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: zone: %w", ToolGetTrustBalance, err)
	}
	g := getTrustBalance{walls: walls, avail: avail, accounts: accounts, loc: loc, now: time.Now}
	t, err := handlerTool(ToolGetTrustBalance, Read, can, g.handle)
	if err != nil {
		return registry.Tool{}, err
	}
	// The cents this tool returns are the figures a later write in the run may carry.
	t, err = pv.ReadTool(t)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	return t, nil
}

type getTrustBalance struct {
	walls    roles.Walls
	avail    TrustAvailable
	accounts MatterTrustAccounts
	loc      *time.Location
	now      func() time.Time
}

type trustAccountOut struct {
	TrustAccountID string `json:"trust_account_id"`
	AvailableCents int64  `json:"available_cents"`
}

type getTrustBalanceOut struct {
	MatterID       string            `json:"matter_id"`
	AsOf           string            `json:"as_of"`
	Accounts       []trustAccountOut `json:"accounts"`
	AvailableCents int64             `json:"available_cents"` // sum over the accounts
}

func (g getTrustBalance) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		MatterID string `json:"matter_id"`
		AsOf     string `json:"as_of"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.MatterID == "" {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, ErrInvalidInput)
	}
	// rule: ARC-I-036
	if err := visibleTo(ctx, g.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	// Available is a balance now; it has no history. A past or future as_of is refused
	// rather than answered with today's figure.
	today := g.now().In(g.loc).Format(time.DateOnly)
	if in.AsOf != "" && in.AsOf != today {
		return registry.ToolResult{}, fmt.Errorf("%s: as_of: %w", ToolGetTrustBalance, ErrInvalidInput)
	}
	ids, err := g.accounts.TrustAccountsOf(ctx, in.MatterID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: trust accounts: %w", ToolGetTrustBalance, err)
	}
	out := getTrustBalanceOut{MatterID: in.MatterID, AsOf: today, Accounts: make([]trustAccountOut, 0, len(ids))}
	for _, id := range ids {
		// rule: ARC-I-036
		// Only the requested, visible matter is read: no other matter id reaches Available.
		cents, err := g.avail.Available(ctx, id, in.MatterID)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: available: %w", ToolGetTrustBalance, err)
		}
		out.Accounts = append(out.Accounts, trustAccountOut{TrustAccountID: id, AvailableCents: cents})
		out.AvailableCents += cents
	}
	// rule: ARC-I-038
	// The result carries ids and cents only: no client or document text, so nothing to quote.
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolGetTrustBalance, err)
	}
	return registry.ToolResult{Output: b}, nil
}
