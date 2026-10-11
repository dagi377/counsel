package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

const closingRunID = "run-closing-checklist"

// closingRead runs a get_trust_balance-shaped read through the run's provenance, as the
// production tool is wrapped (pv.ReadTool), so availableCents is a figure the run read.
func closingRead(t *testing.T, pv *agenttools.Provenance, matterID string, availableCents int64) {
	t.Helper()
	out, err := json.Marshal(map[string]any{
		"matter_id": matterID, "as_of": "2026-10-05",
		"accounts":        []map[string]any{{"trust_account_id": "trust-1", "available_cents": availableCents}},
		"available_cents": availableCents,
	})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := pv.ReadTool(registry.Tool{
		Name: agent.ToolGetTrustBalance, Kind: registry.ToolRead,
		Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
			return registry.ToolResult{Output: out}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := registry.Principal{Kind: registry.PrincipalAgent, RunID: closingRunID}
	if _, err := tool.Handle(context.Background(), p, json.RawMessage(`{"matter_id":"`+matterID+`"}`)); err != nil {
		t.Fatal(err)
	}
}

// rule: ARC-U-027
// rule: BIL-L-036
// The closing checklist carries the trust balance get_trust_balance returned, signed and
// unclamped; any non-zero balance blocks the close, and a balance the run never read fails.
func TestClosingChecklist(t *testing.T) {
	const matterID = "11111111-1111-4111-8111-111111111111"
	tests := []struct {
		name         string
		read, filled int64
		wantErr      error
		wantBlockers []string
	}{
		{"zero balance closes", 0, 0, nil, nil},
		{"money in trust blocks", 125000, 125000, nil, []string{platform.BlockerTrustBalance}},
		{"overdrawn ledger blocks, not clamped", -4200, -4200, nil, []string{platform.BlockerTrustBalance}},
		{"invented balance", 125000, 0, agenttools.ErrUnsourcedValue, nil},
		{"invented clamp of an overdraft", -4200, 0, agenttools.ErrUnsourcedValue, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			closingRead(t, pv, matterID, tt.read)
			c, err := platform.NewClosingChecklist(pv, closingRunID, matterID, tt.filled)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if !slices.Equal(c.Blockers, tt.wantBlockers) || c.Ready() != (len(tt.wantBlockers) == 0) {
				t.Fatalf("blockers = %v ready %v, want %v", c.Blockers, c.Ready(), tt.wantBlockers)
			}
			raw, err := c.Payload()
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				MatterID          string `json:"matter_id"`
				TrustBalanceCents int64  `json:"trust_balance_cents"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.MatterID != matterID || got.TrustBalanceCents != tt.read {
				t.Fatalf("payload = %+v, want %s %d", got, matterID, tt.read)
			}
		})
	}
}
