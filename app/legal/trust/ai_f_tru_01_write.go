package trust

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// EventBankLinesImported is the event a bank statement import publishes (POST
// /trust/bank-lines/imports). It starts F-TRU-01 next to its daily schedule.
const EventBankLinesImported = "bank_line.imported"

// Tool names the F-TRU-01 write path calls. They are the a2 tools of the Def; package trust
// cannot import package agent, so they are named here.
const (
	toolReportPlan        = "report_plan"
	toolDraftDocument     = "draft_document"
	toolSubmitForApproval = "submit_for_approval"
)

// bankFeedMethods are the payment methods a receipt draft may name; any other is "other".
var bankFeedMethods = []string{"cheque", "wire", "eft", "bank_draft", "cash", "other"}

// BankFeedSyncBinding binds F-TRU-01 to its daily schedule and to a bank statement import. A
// scheduled run has no event user: it acts for the feature's designated lawyer from firm
// settings, and with none named it does not start. The screen actions run it on request.
func BankFeedSyncBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(BankFeedSyncDef(), EventBankLinesImported)
}

// ToolCaller runs one agent tool through the registry: capability, input schema, plan before
// write and run scope are checked there (agenttools.Registry).
type ToolCaller interface {
	Call(ctx context.Context, p agenttools.Principal, name string, in json.RawMessage) (agenttools.ToolResult, error)
}

// ReceiptGates is F-TRU-01's write path, the ReceiptProposals sink of BankFeedPipeline. For
// each proposed receipt it drafts a receipt proposal with draft_document and opens gate
// trust.receipt_classification (G6) on it with submit_for_approval, through the registry as
// the run's agent principal, so the acting user is the preparer and the run scope holds it to
// the Def's gate kind. It never posts or classifies a receipt. Match suggestions are returned
// to the screen only and open nothing.
type ReceiptGates struct {
	Tools ToolCaller
	// Run is the run's agent principal: its run id, acting user and grant.
	Run agenttools.Principal
	// MatterID is the matter the run's trigger selected; the drafts are filed on it.
	MatterID string
	// Sources are the run's quoted bank-feed lines: every amount and date a receipt carries
	// must be one its cited line states.
	Sources []agenttools.Quoted
}

// ProposeReceipts implements ReceiptProposals. It checks every receipt before the first
// write, so one unsourced or malformed receipt writes nothing. A missing port, a principal
// that is not the run's agent, or a ctx without the run's scope fails closed.
func (g ReceiptGates) ProposeReceipts(ctx context.Context, featureID string, items []ProposedReceipt) error {
	// rule: AI-R-003
	// Fail closed: every port, the run's agent principal and its scope are required.
	if g.Tools == nil || g.MatterID == "" || len(g.Sources) == 0 {
		return fmt.Errorf("%s: receipt gates: %w", featureID, ErrPortMissing)
	}
	if g.Run.Kind != registry.PrincipalAgent || g.Run.RunID == "" || g.Run.ActingFor == "" {
		return fmt.Errorf("%s: receipt gates: not the run's agent: %w", featureID, ErrPortMissing)
	}
	// rule: ARC-I-035
	// The gate opens only inside the run's scope, which allows the Def's gate kinds.
	if s, ok := agenttools.ScopeOf(ctx); !ok || s.RunID != g.Run.RunID || !s.AllowsGateKind(GateReceiptClassification) {
		return fmt.Errorf("%s: receipt gates: %w", featureID, agenttools.ErrGateKindNotAllowed)
	}
	var receipts []ProposedReceipt
	for i, r := range items {
		if r.Kind != ItemReceipt {
			continue
		}
		if err := g.sourced(r); err != nil {
			return fmt.Errorf("%s: item %d: %w", featureID, i, err)
		}
		receipts = append(receipts, r)
	}
	if len(receipts) == 0 {
		return nil
	}
	// rule: ARC-I-039
	// The plan is reported before the first write.
	plan, err := json.Marshal(map[string]any{"steps": []map[string]string{
		{"step_id": "draft", "title": "Draft the proposed trust receipts"},
		{"step_id": "submit", "title": "Submit each for receipt classification"},
	}})
	if err != nil {
		return fmt.Errorf("%s: plan: %w", featureID, err)
	}
	if _, err := g.Tools.Call(ctx, g.Run, toolReportPlan, plan); err != nil {
		return fmt.Errorf("%s: %s: %w", featureID, toolReportPlan, err)
	}
	for i, r := range receipts {
		if err := g.propose(ctx, r); err != nil {
			return fmt.Errorf("%s: receipt %d: %w", featureID, i, err)
		}
	}
	// rule: AI-R-005
	// Only the feature id and the count are logged, never amounts, dates or bank-feed text.
	slog.InfoContext(ctx, "trust receipt gates opened", "feature", featureID, "receipts", len(receipts))
	return nil
}

// sourced checks r against the line it cites: a positive amount in integer cents and a
// calendar date, both stated by that line.
func (g ReceiptGates) sourced(r ProposedReceipt) error {
	src, ok := citedSource(r.Citation, g.Sources)
	// rule: ARC-I-038
	if !ok {
		return fmt.Errorf("citation: %w", ErrBadReceipt)
	}
	// rule: BIL-L-034
	if r.AmountCents <= 0 {
		return fmt.Errorf("amount: %w", ErrBadReceipt)
	}
	if _, err := time.Parse(time.DateOnly, r.ReceivedOn); err != nil {
		return fmt.Errorf("received on: %w", ErrBadReceipt)
	}
	// rule: ARC-U-027
	// The amount and the date are the cited line's, never figures the model worked out.
	if !slices.Contains(centsFigures(src.Text), r.AmountCents) || !strings.Contains(src.Text, r.ReceivedOn) {
		return fmt.Errorf("receipt: %w", agenttools.ErrUnsourcedValue)
	}
	return nil
}

// propose drafts one receipt and opens its gate.
func (g ReceiptGates) propose(ctx context.Context, r ProposedReceipt) error {
	draft, err := json.Marshal(map[string]string{
		"matter_id": g.MatterID, "title": "Proposed trust receipt", "body_markdown": ReceiptBody(r),
	})
	if err != nil {
		return fmt.Errorf("draft: %w", err)
	}
	res, err := g.Tools.Call(ctx, g.Run, toolDraftDocument, draft)
	if err != nil {
		return fmt.Errorf("%s: %w", toolDraftDocument, err)
	}
	var out agenttools.WriteOutput
	if err := json.Unmarshal(res.Output, &out); err != nil || out.Ref == "" {
		return fmt.Errorf("%s: no draft: %w", toolDraftDocument, ErrBadReceipt)
	}
	// rule: AI-R-003
	// The gate is the classification a person other than the preparer decides.
	submit, err := json.Marshal(map[string]string{
		"subject_type": "document", "subject_id": out.Ref, "gate_kind": GateReceiptClassification,
	})
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	if _, err := g.Tools.Call(ctx, g.Run, toolSubmitForApproval, submit); err != nil {
		return fmt.Errorf("%s: %w", toolSubmitForApproval, err)
	}
	return nil
}

// ReceiptBody is the draft text of one proposed receipt: its amount in integer cents, its
// date, its method and the bank-feed line it cites. Bank-feed text (the reference) stays out:
// the person reads it on the cited line.
func ReceiptBody(r ProposedReceipt) string {
	method := r.Method
	if !slices.Contains(bankFeedMethods, method) {
		method = "other"
	}
	return fmt.Sprintf("Proposed trust receipt for classification (G6).\n\n"+
		"- Amount: %d cents\n- Received on: %s\n- Method: %s\n- Source: bank feed %s, page %d, lines %d-%d\n\n"+
		"A person other than the preparer classifies and posts it.",
		r.AmountCents, r.ReceivedOn, method, r.Citation.VersionID, r.Citation.Page, r.Citation.FromLine, r.Citation.ToLine)
}
