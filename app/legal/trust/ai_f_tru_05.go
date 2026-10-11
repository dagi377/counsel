package trust

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
)

// FeatureRetainerReplenishment is the spec id of "retainer-replenishment".
const FeatureRetainerReplenishment = "F-TRU-05"

// EventLedgerBelowFloor is the event of a client trust ledger falling below the retainer
// floor agreed in the engagement (docs/QUESTIONS.md Q-FEAT-TRU-05).
const EventLedgerBelowFloor = "trust.ledger_below_floor"

// GateOutgoingEmail is G-OUT, agent.GateOutgoingEmail. agent imports trust, so the kind is
// restated here; a test holds the two equal.
const GateOutgoingEmail = "platform.outgoing_email"

// RetainerReplenishmentDef is F-TRU-05: when a client ledger falls below the agreed retainer
// floor, the agent reads the matter, its trust balance and its open bills and drafts a
// top-up request to the client with draft_email. Submitting the draft opens
// platform.outgoing_email; nothing is sent until a lawyer other than the preparer approves.
func RetainerReplenishmentDef() feature.Def {
	return feature.Def{
		ID:      FeatureRetainerReplenishment,
		Area:    "trust",
		Name:    "retainer-replenishment",
		Source:  feature.SourceJob,
		Trigger: "event (ledger below the agreed floor)",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "get_trust_balance", "get_bill",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// The request is only drafted: draft_email with submit opens G-OUT, never sends.
		GateKinds:  []string{GateOutgoingEmail},
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     prompts,
		PromptPath: "prompts/f-tru-05.md",
		Fixture:    "testdata/ai/f-tru-05/retainer-replenishment",
	}
}

// RetainerReplenishmentBinding binds F-TRU-05 to a ledger falling below its floor. Event runs
// act for the event's user, so it needs no scheduled acting user.
func RetainerReplenishmentBinding() (trigger.Binding, error) {
	return trigger.Bind(RetainerReplenishmentDef(), "", EventLedgerBelowFloor)
}

// RetainerShortfall is what a client must deposit to bring a trust ledger back to the agreed
// floor. Counsel computes it, never the model. Every figure is signed integer cents.
type RetainerShortfall struct {
	MatterID       string
	BalanceCents   int64 // the ledger's signed balance; below zero is overdrawn
	FloorCents     int64 // the retainer floor agreed in the engagement
	Overdrawn      bool  // BalanceCents < 0
	ShortfallCents int64 // FloorCents - BalanceCents, always > 0
}

// NewRetainerShortfall computes matterID's shortfall from its signed balance and the agreed
// floor. A floor that is not positive, or a balance at or above the floor, is ErrInvalid:
// there is nothing to request.
func NewRetainerShortfall(matterID string, balanceCents, floorCents int64) (RetainerShortfall, error) {
	if matterID == "" || floorCents <= 0 || balanceCents >= floorCents {
		return RetainerShortfall{}, fmt.Errorf("retainer shortfall: %w", ErrInvalid)
	}
	// rule: BIL-L-036
	// An overdraft is never hidden: the balance stays signed, Overdrawn says so, and the
	// shortfall covers the overdraft as well as the floor.
	return RetainerShortfall{
		MatterID: matterID, BalanceCents: balanceCents, FloorCents: floorCents,
		Overdrawn: balanceCents < 0, ShortfallCents: floorCents - balanceCents,
	}, nil
}

// Observe records the shortfall's figures into runID's provenance, so the request may state
// the floor and the shortfall Counsel computed. Without a provenance it fails closed with
// ErrPortMissing, and the run must not start.
func (s RetainerShortfall) Observe(pv *agenttools.Provenance, runID string) error {
	// rule: AI-R-003
	if pv == nil || runID == "" {
		return fmt.Errorf("retainer shortfall: %w", ErrPortMissing)
	}
	b, err := json.Marshal(s.figures())
	if err != nil {
		return fmt.Errorf("retainer shortfall: %w", err)
	}
	// rule: ARC-U-027
	// The figures come from Counsel's ledger and the engagement, not from the model.
	pv.Observe(runID, b)
	return nil
}

// Prompt is the run's request text: ids, signed figures and the overdrawn flag only, never
// client text (AI-R-005).
func (s RetainerShortfall) Prompt() string {
	return "Retainer below the agreed floor for matter " + s.MatterID + ": " +
		"balance_cents=" + strconv.FormatInt(s.BalanceCents, 10) + " " +
		"overdrawn=" + strconv.FormatBool(s.Overdrawn) + " " +
		"floor_cents=" + strconv.FormatInt(s.FloorCents, 10) + " " +
		"shortfall_cents=" + strconv.FormatInt(s.ShortfallCents, 10)
}

type shortfallFigures struct {
	BalanceCents   int64 `json:"balance_cents"`
	FloorCents     int64 `json:"floor_cents"`
	ShortfallCents int64 `json:"shortfall_cents"`
}

func (s RetainerShortfall) figures() shortfallFigures {
	return shortfallFigures{BalanceCents: s.BalanceCents, FloorCents: s.FloorCents, ShortfallCents: s.ShortfallCents}
}
