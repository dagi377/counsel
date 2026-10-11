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
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// FeatureEvergreenWatch is the spec id of "Evergreen watch".
const FeatureEvergreenWatch = "F-BIL-05"

// ActionDraftRetainerRequest is the retainer-requests screen action that runs F-BIL-05 on
// request.
const ActionDraftRetainerRequest = "retainer-requests > Draft request"

//go:embed prompts/f-bil-05.md
var promptFBIL05 embed.FS

// EvergreenWatchDef is F-BIL-05: every business day the agent reads each matter's trust
// balance, unbilled work and open bills and, where an evergreen retainer has run low, drafts a
// retainer request to the client with draft_email and submit, which opens
// platform.outgoing_email (G-OUT). It prepares only: nothing is sent and no trust money moves
// until a person other than the preparer approves.
func EvergreenWatchDef() feature.Def {
	return feature.Def{
		ID:      FeatureEvergreenWatch,
		Area:    "billing",
		Name:    "Evergreen watch",
		Source:  feature.SourceJob,
		Trigger: "schedule daily",
		Actions: []string{ActionDraftRetainerRequest},
		Kind:    ai.KindToolUse,
		Tools: []string{
			"get_matter", "list_unbilled", "get_bill", "get_trust_balance",
			"draft_email", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		GateKinds: []string{agent.GateOutgoingEmail}, // G-OUT, opened by draft_email with submit
		// The run's agent principal holds the capabilities of its tools and no other.
		Grant:      []string{"agent.ask", "billing.view", "documents.edit", "matters.view", "trust.view"},
		Prompt:     promptFBIL05,
		PromptPath: "prompts/f-bil-05.md",
		Fixture:    "testdata/ai/f-bil-05/evergreen-watch",
	}
}

// EvergreenWatchBinding binds F-BIL-05 to its daily schedule. A scheduled run has no event
// user, so it acts for the feature's designated lawyer from firm settings.
func EvergreenWatchBinding() (trigger.Binding, error) {
	return trigger.BindDesignated(EvergreenWatchDef())
}

// moneyDollars matches a dollar figure in email text, "$1,234.56" or "$120".
var moneyDollars = regexp.MustCompile(`\$\s?(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d{2}))?`)

// moneyCents matches a figure written in cents, "84750 cents".
var moneyCents = regexp.MustCompile(`(\d{1,3}(?:,\d{3})+|\d+)\s*cents\b`)

// EmailAmounts returns every money figure in text, in integer cents: dollar figures
// ("$1,234.56") and figures written in cents ("84750 cents").
func EmailAmounts(text string) []int64 {
	var out []int64
	for _, m := range moneyDollars.FindAllStringSubmatch(text, -1) {
		d, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
		if err != nil {
			continue
		}
		c := int64(0)
		if m[2] != "" {
			c, _ = strconv.ParseInt(m[2], 10, 64)
		}
		out = append(out, d*100+c)
	}
	for _, m := range moneyCents.FindAllStringSubmatch(text, -1) {
		if c, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// SourcedEmailHandlers returns hs with draft_email guarded for the billing features: for an
// agent principal every money figure in the email's subject and body must be a cents value a
// read tool of the same run returned (get_bill, list_unbilled, get_trust_balance), else the
// call fails with agenttools.ErrUnsourcedValue before any draft or gate row exists. get_bill
// and get_trust_balance record their figures already; list_unbilled's are recorded here. Both
// wrappers read the run's *agenttools.Provenance from Deps; without one they fail closed.
func SourcedEmailHandlers(hs agent.HandlerSet) (agent.HandlerSet, error) {
	var out []agent.Handler
	for _, name := range hs.Names() {
		h, _ := hs.For(name)
		switch name {
		case agent.ToolDraftEmail:
			h = sourcedEmail(h)
		case agent.ToolListUnbilled:
			h = observedRead(h)
		}
		out = append(out, h)
	}
	return agent.NewHandlerSet(out...)
}

// sourcedEmail wraps the draft_email handler with the amount provenance check.
func sourcedEmail(h agent.Handler) agent.Handler {
	build := h.Build
	return agent.Handler{Name: h.Name, Build: func(d agent.Deps) (registry.Tool, error) {
		pv, err := agent.DepOf[*agenttools.Provenance](d)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", h.Name, err)
		}
		t, err := build(d)
		if err != nil {
			return registry.Tool{}, err
		}
		next := t.Handle
		t.Handle = func(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
			if p.Kind == registry.PrincipalAgent {
				if err := checkEmailAmounts(pv, p.RunID, raw); err != nil {
					return registry.ToolResult{}, fmt.Errorf("%s: %w", h.Name, err)
				}
			}
			return next(ctx, p, raw)
		}
		return t, nil
	}}
}

// observedRead wraps a read handler so its results' figures enter the run's provenance.
func observedRead(h agent.Handler) agent.Handler {
	build := h.Build
	return agent.Handler{Name: h.Name, Build: func(d agent.Deps) (registry.Tool, error) {
		pv, err := agent.DepOf[*agenttools.Provenance](d)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", h.Name, err)
		}
		t, err := build(d)
		if err != nil {
			return registry.Tool{}, err
		}
		t, err = pv.ReadTool(t)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", h.Name, err)
		}
		return t, nil
	}}
}

// checkEmailAmounts fails with agenttools.ErrUnsourcedValue unless every money figure in the
// email input is one runID's read tools returned.
func checkEmailAmounts(pv *agenttools.Provenance, runID string, raw json.RawMessage) error {
	var in struct {
		Subject string `json:"subject"`
		Body    string `json:"body_markdown"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return fmt.Errorf("amounts: %w", agenttools.ErrInvalidInput)
	}
	amounts := EmailAmounts(in.Subject + "\n" + in.Body)
	if len(amounts) == 0 {
		return nil
	}
	b, err := json.Marshal(map[string][]int64{"amounts_cents": amounts})
	if err != nil {
		return fmt.Errorf("amounts: %w", err)
	}
	// rule: ARC-U-027
	// A figure the model worked out or invented is not one the run read.
	return pv.Check(runID, []string{"/amounts_cents/*"}, b)
}
