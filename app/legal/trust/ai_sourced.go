package trust

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// The trust AI features draft prose that carries trust figures to the regulator or to a
// client: a reconciliation package, a worksheet, a top-up request, a spot-audit pack.
// draft_document and draft_email carry those amounts inside free text, where no JSON pointer
// reaches them, so the shared Provenance check cannot see them. SourcedCents closes that gap:
// every amount and date a trust draft states must be a value a read tool returned in the same
// run. It is the one trust sourced-figures check; boot wraps every trust feature's draft tools
// with it (SourcedFeatures).

// SourcedFeatures lists the trust features whose draft_document and draft_email boot wraps
// with SourcedCents in production. A trust feature that drafts adds its id here; the boot
// guard test fails when one is missing.
func SourcedFeatures() []string {
	return []string{
		FeatureBankFeedSync,
		FeatureMonthEndReconciliation,
		FeatureRetainerReplenishment,
		FeatureAnnualReportPrep,
		FeatureRetainerCoverageScan,
		FeatureSpotAuditPack,
	}
}

var (
	// sourcedCentsText is one amount written as the trust prompts require it: an optional
	// minus sign, an integer, then "cents" ("125000 cents", "-4200 cents"). Thousands
	// separators are read; a fraction is captured so it can be refused.
	sourcedCentsText = regexp.MustCompile(`(?i)(-?)\b(\d[\d,_]*(?:\.\d+)?)\s*cents\b`)
	// unsourceableText is an amount the check cannot compare against the run's integer cents:
	// a dollar sign, the word dollars, or a decimal dollars-and-cents figure ("1,250.00").
	unsourceableText = regexp.MustCompile(`(?i)\$\s*-?\d|\d\s*dollars\b|\b(?:\d{1,3}(?:,\d{3})+|\d+)\.\d{2}\b`)
	// dateText is a calendar date as text carries it.
	dateText = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
)

// CheckSourcedCents checks free text as SourcedCents checks a draft's text fields.
func CheckSourcedCents(pv *agenttools.Provenance, runID, text string) error {
	in, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("trust draft: %w", err)
	}
	return checkSourced(pv, runID, in)
}

// SourcedCents wraps a trust feature's write tool (draft_document, draft_email) so that, for
// an agent principal, every amount and date its input states is one a read tool returned in
// the same run (pv): a number field named "*_cents", an amount in any text field ("125000
// cents", signed) or a YYYY-MM-DD date. An invented, rounded, worked-out or converted amount,
// a fraction of a cent, an amount in dollars, or an overdrawn balance written without its
// sign is agenttools.ErrUnsourcedValue and the handler never runs, so no draft and no gate is
// made. A user principal (the manual path) is not checked. A nil pv fails with
// ErrPortMissing and agenttools.ErrUnsourcedValue; a tool that is not a write tool fails with
// ErrPortMissing and agenttools.ErrBadTool.
func SourcedCents(pv *agenttools.Provenance, t registry.Tool) (registry.Tool, error) {
	// rule: AI-R-003
	// Fail closed: a trust write without the run's provenance is never built.
	if t.Kind != registry.ToolWrite || t.Handle == nil {
		return registry.Tool{}, fmt.Errorf("sourced cents %q: %w: %w", t.Name, ErrPortMissing, agenttools.ErrBadTool)
	}
	// rule: ARC-U-027
	if pv == nil {
		return registry.Tool{}, fmt.Errorf("sourced cents %q: no provenance: %w: %w", t.Name, ErrPortMissing, agenttools.ErrUnsourcedValue)
	}
	h := t.Handle
	name := t.Name
	t.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
		if p.Kind == registry.PrincipalAgent {
			// rule: ARC-U-027
			if err := checkSourced(pv, p.RunID, in); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", name, err)
			}
		}
		return h(ctx, p, in)
	}
	return t, nil
}

// checkSourced checks every amount and date in a write input against runID's provenance. It
// fails closed: no Provenance or no run id is unsourced. Errors name the field kind only,
// never the value (it may be a client amount).
func checkSourced(pv *agenttools.Provenance, runID string, in json.RawMessage) error {
	// rule: ARC-U-027
	// Without the run's read values no amount can be shown to be sourced.
	if pv == nil || runID == "" {
		return fmt.Errorf("trust draft: no provenance: %w", agenttools.ErrUnsourcedValue)
	}
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("trust draft: %w", agenttools.ErrInvalidInput)
	}
	var stated statedValues
	stated.walk(v, "")
	// rule: ARC-U-027
	// Every amount a trust draft states is integer cents: a dollar amount or a fraction of a
	// cent is never checked against the run's figures, it is refused.
	if stated.notCents {
		return fmt.Errorf("trust draft: amount not in integer cents: %w", agenttools.ErrUnsourcedValue)
	}
	if len(stated.Cents) == 0 && len(stated.Dates) == 0 {
		return nil
	}
	b, err := json.Marshal(stated)
	if err != nil {
		return fmt.Errorf("trust draft: %w", err)
	}
	// rule: ARC-U-027
	// rule: BIL-L-036
	// Amounts are signed: an overdrawn ledger read as -5000 cents is never written as 5000,
	// so a draft cannot hide an overdraft.
	if err := pv.Check(runID, []string{"/cents/*", "/dates/*"}, b); err != nil {
		return fmt.Errorf("trust draft: %w", err)
	}
	return nil
}

// statedValues is every amount and date a write input states.
type statedValues struct {
	Cents    []json.Number `json:"cents"`
	Dates    []string      `json:"dates"`
	notCents bool          // the input states an amount that is not integer cents
}

// walk collects the amounts and dates v states; key is the field v sits in.
func (s *statedValues) walk(v any, key string) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			s.walk(c, k)
		}
	case []any:
		for _, c := range x {
			s.walk(c, key)
		}
	case json.Number:
		if strings.HasSuffix(key, "_cents") {
			if _, err := strconv.ParseInt(x.String(), 10, 64); err != nil {
				s.notCents = true
				return
			}
			s.Cents = append(s.Cents, x)
		}
	case string:
		s.text(x)
	}
}

// text collects the amounts and dates of one text value.
func (s *statedValues) text(x string) {
	if unsourceableText.MatchString(x) {
		s.notCents = true
	}
	for _, m := range sourcedCentsText.FindAllStringSubmatch(x, -1) {
		lit := strings.NewReplacer(",", "", "_", "").Replace(m[2])
		n, err := strconv.ParseInt(lit, 10, 64)
		if err != nil {
			// A fraction of a cent, or a figure too large to be cents: refused, never checked.
			s.notCents = true
			continue
		}
		s.Cents = append(s.Cents, json.Number(strconv.FormatInt(signed(m[1], n), 10)))
	}
	s.Dates = append(s.Dates, dateText.FindAllString(x, -1)...)
}
