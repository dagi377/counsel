package billing

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// FeatureDraftCreditNote is the spec id of "Draft credit note".
const FeatureDraftCreditNote = "F-BIL-08"

// ActionDraftCreditNote is the credit-notes screen action that runs F-BIL-08.
const ActionDraftCreditNote = "credit-notes > Draft credit note"

// GateCreditNote is the gate kind of spec gate G-B5: a person approves a credit note before
// it is numbered and issued. It is a tool-gap kind: draft_document then submit_for_approval
// opens it.
const GateCreditNote = "billing.credit_note"

//go:embed prompts/f-bil-08.md
var promptFBIL08 embed.FS

// DraftCreditNoteDef is F-BIL-08: on request from the credit-notes screen the agent reads the
// selected bill, drafts a credit note with draft_document and opens billing.credit_note
// (G-B5) with submit_for_approval. The credit note stays a draft until a person other than
// the acting user approves it; its tax is computed by the billing tax engine (place of
// supply) when it is issued, never by the agent.
func DraftCreditNoteDef() feature.Def {
	return feature.Def{
		ID:      FeatureDraftCreditNote,
		Area:    "billing",
		Name:    "Draft credit note",
		Source:  feature.SourceScreenAction,
		Trigger: "on request: " + ActionDraftCreditNote,
		Actions: []string{ActionDraftCreditNote},
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{GateCreditNote},
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"matters.view", "billing.view", "trust.view", "documents.edit", "agent.ask"},
		Prompt:     promptFBIL08,
		PromptPath: "prompts/f-bil-08.md",
		Fixture:    "testdata/ai/f-bil-08/draft-credit-note",
	}
}

// creditFigure is a money figure in a credit note body: a dollar amount ("$1,130.00",
// "$56"), or a figure with thousands separators or cents ("1,130", "565.00"). Dates and ids
// do not match.
var creditFigure = regexp.MustCompile(`\$\s?\d[\d,]*(?:\.\d{2})?|\b\d{1,3}(?:,\d{3})+(?:\.\d{2})?\b|\b\d+\.\d{2}\b`)

// CreditNoteCents returns every money figure in body, in integer cents, in order. A figure
// with no cents counts as whole dollars.
func CreditNoteCents(body string) ([]int64, error) {
	var out []int64
	for _, f := range creditFigure.FindAllString(body, -1) {
		s := strings.NewReplacer("$", "", ",", "", " ", "").Replace(f)
		whole, frac, _ := strings.Cut(s, ".")
		d, err := strconv.ParseInt(whole, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("credit note figure: %w", agent.ErrInvalidInput)
		}
		c := int64(0)
		if frac != "" {
			if c, err = strconv.ParseInt(frac, 10, 64); err != nil {
				return nil, fmt.Errorf("credit note figure: %w", agent.ErrInvalidInput)
			}
		}
		out = append(out, d*100+c)
	}
	return out, nil
}

// CheckCreditNote returns agenttools.ErrUnsourcedValue unless every money figure in the
// draft_document input's body is one a read tool returned in run runID (pv holds them). A
// body that states no amount is not a credit note (agent.ErrInvalidInput).
func CheckCreditNote(pv *agenttools.Provenance, runID string, input json.RawMessage) error {
	var in struct {
		BodyMarkdown string `json:"body_markdown"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Errorf("credit note: %w", agent.ErrInvalidInput)
	}
	cents, err := CreditNoteCents(in.BodyMarkdown)
	if err != nil {
		return err
	}
	if len(cents) == 0 {
		return fmt.Errorf("credit note states no amount: %w", agent.ErrInvalidInput)
	}
	raw, err := json.Marshal(map[string][]int64{"amounts": cents})
	if err != nil {
		return fmt.Errorf("credit note: %w", err)
	}
	// rule: ARC-U-027
	// Every amount in the credit note is a figure the run read (get_bill, list_unbilled or
	// get_trust_balance); a made-up amount never reaches a draft or a gate.
	if err := pv.Check(runID, []string{"/amounts/*"}, raw); err != nil {
		return fmt.Errorf("credit note: %w", err)
	}
	return nil
}

// CreditNoteWrap is the agent.ToolWrap boot puts on draft_document for F-BIL-08 runs:
// CheckCreditNote ahead of the built tool for an agent principal, so an unsourced amount
// stores no draft and opens no gate. It fails closed: without the run's Provenance in d it
// does not build (agent.ErrMissingDep).
func CreditNoteWrap(d agent.Deps, t registry.Tool) (registry.Tool, error) {
	pv, err := agent.DepOf[*agenttools.Provenance](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s credit note: %w", t.Name, err)
	}
	next := t.Handle
	t.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
		if p.Kind == registry.PrincipalAgent {
			// rule: ARC-U-027
			if err := CheckCreditNote(pv, p.RunID, in); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", t.Name, err)
			}
		}
		return next(ctx, p, in)
	}
	return t, nil
}
