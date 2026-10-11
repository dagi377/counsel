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
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// FeatureBillRender is the spec id of "Bill render and pre-flight".
const FeatureBillRender = "F-BIL-02"

// EventGateDecided is Counsel's gate.decided event; F-BIL-02 starts when a G-B1 pre-bill
// review closes.
const EventGateDecided = "gate.decided"

//go:embed prompts/f-bil-02.md
var promptFBIL02 embed.FS

// BillRenderDef is F-BIL-02: when a pre-bill review (G-B1) closes, the agent reads the bill,
// runs the pre-flight checks and drafts the rendered bill with draft_document, then submits
// it for billing.bill_approval (G-B2, a tool-gap kind opened through submit_for_approval).
// The bill stays a draft until a lawyer other than the acting user approves it.
func BillRenderDef() feature.Def {
	return feature.Def{
		ID:      FeatureBillRender,
		Area:    "billing",
		Name:    "Bill render and pre-flight",
		Source:  feature.SourceJob,
		Trigger: "event: G-B1 closed",
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_document", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{bill.GateBillApproval}, // G-B2, opened by submit_for_approval
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     promptFBIL02,
		PromptPath: "prompts/f-bil-02.md",
		Fixture:    "testdata/ai/f-bil-02/bill-render-and-pre-flight",
	}
}

// BillRenderBinding binds F-BIL-02 to gate.decided, acting for the event's user; the run
// renders only when the decided gate is a G-B1 pre-bill review.
func BillRenderBinding() (trigger.Binding, error) {
	return trigger.Bind(BillRenderDef(), "", EventGateDecided)
}

// centsFigure is an amount as a bill draft states it in prose: "113000 cents".
var centsFigure = regexp.MustCompile(`(\d[\d,_]*)\s*cents\b`)

// SourcedBillDraftHandler is draft_document's handler with the F-BIL-02 money check: for an
// agent run, every "<n> cents" figure in the draft's title or body must be one a read tool
// returned in the run (agenttools.ErrUnsourcedValue otherwise), checked before the draft is
// stored, so an invented amount leaves no draft and no gate. draft_document's input has no
// cents field, so this is the provenance check of a rendered bill.
func SourcedBillDraftHandler() agent.Handler {
	base := agent.DraftDocumentHandler()
	return agent.Handler{Name: base.Name, Build: func(d agent.Deps) (registry.Tool, error) {
		tool, err := base.Build(d)
		if err != nil {
			return registry.Tool{}, err
		}
		pv, err := agent.DepOf[*agenttools.Provenance](d)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", base.Name, err)
		}
		next := tool.Handle
		// rule: ARC-U-027
		tool.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			if p.Kind == registry.PrincipalAgent {
				if err := checkCentsSourced(pv, p.RunID, in); err != nil {
					return registry.ToolResult{}, fmt.Errorf("%s: %w", base.Name, err)
				}
			}
			return next(ctx, p, in)
		}
		return tool, nil
	}}
}

// checkCentsSourced checks every cents figure in the draft's title and body against runID's
// provenance. Only the field name is reported, never the figure.
func checkCentsSourced(pv *agenttools.Provenance, runID string, in json.RawMessage) error {
	var d struct {
		Title string `json:"title"`
		Body  string `json:"body_markdown"`
	}
	if err := json.Unmarshal(in, &d); err != nil {
		return fmt.Errorf("draft: %w", agenttools.ErrInvalidInput)
	}
	for _, m := range centsFigure.FindAllStringSubmatch(d.Title+"\n"+d.Body, -1) {
		n, err := strconv.ParseInt(strings.NewReplacer(",", "", "_", "").Replace(m[1]), 10, 64)
		if err != nil {
			return fmt.Errorf("draft amount: %w", agenttools.ErrUnsourcedValue)
		}
		v, err := json.Marshal(map[string]int64{"cents": n})
		if err != nil {
			return err
		}
		if err := pv.Check(runID, []string{"/cents"}, v); err != nil {
			return fmt.Errorf("draft amount: %w", err)
		}
	}
	return nil
}
