package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dagi377/counsel/kernel/registry"
)

// ErrUnsourcedValue: a write tool's input carries an amount or date that no read tool returned
// in the same run, so the model made it up (422 unsourced_value).
var ErrUnsourcedValue = errors.New("agenttools: unsourced_value")

// dateValue is a calendar date as read tools return it (YYYY-MM-DD).
var dateValue = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Inside quoted text: a calendar date, and a number (digits, optional thousands commas and
// decimals). Dates are taken out before numbers, so 2026-10-05 adds no 2026, 10 or 5.
var (
	textDate   = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	textNumber = regexp.MustCompile(`\d{1,3}(?:,\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?`)
)

// Provenance keeps, per agent run, the numbers and dates read tools returned to the model. It
// stores values only: no field names, tool names or any other text from the result. A write
// tool that declares provenance fields may only carry values from its run's set.
type Provenance struct {
	mu   sync.Mutex
	runs map[string]map[string]struct{}
}

// NewProvenance returns an empty Provenance.
func NewProvenance() *Provenance {
	return &Provenance{runs: map[string]map[string]struct{}{}}
}

// Observe records every number and date in a read result's JSON output against runID,
// including the dates and numbers written inside the text of an untrusted envelope (a
// Quoted value, a read_document page line), so a value the run read in a quote counts as read. An output
// that is not JSON records nothing.
func (pv *Provenance) Observe(runID string, output json.RawMessage) {
	if runID == "" || len(bytes.TrimSpace(output)) == 0 {
		return
	}
	v, err := decodeNumbers(output)
	if err != nil {
		return
	}
	var keys []string
	collectValues(v, &keys)
	pv.add(runID, keys)
}

// ObserveTrigger records the values of a run's trigger payload as read: the event's dates,
// amounts and ids reach the model as quoted trigger data, so a write may use them (a bill
// period sourced from the event's date). A JSON payload records every number and date in
// it; any other text records the dates and numbers it states.
func (pv *Provenance) ObserveTrigger(runID, payload string) {
	if runID == "" || strings.TrimSpace(payload) == "" {
		return
	}
	// rule: ARC-U-027
	if json.Valid([]byte(payload)) {
		pv.Observe(runID, json.RawMessage(payload))
		return
	}
	var keys []string
	collectQuoted(payload, &keys)
	pv.add(runID, keys)
}

// add records keys against runID.
func (pv *Provenance) add(runID string, keys []string) {
	if len(keys) == 0 {
		return
	}
	pv.mu.Lock()
	defer pv.mu.Unlock()
	set := pv.runs[runID]
	if set == nil {
		set = map[string]struct{}{}
		pv.runs[runID] = set
	}
	for _, k := range keys {
		set[k] = struct{}{}
	}
}

// Forget drops the run's set once the run has ended.
func (pv *Provenance) Forget(runID string) {
	pv.mu.Lock()
	defer pv.mu.Unlock()
	delete(pv.runs, runID)
}

// Check returns ErrUnsourcedValue when a value at one of the JSON pointers in input is not in
// runID's set. A pointer that resolves to nothing, or to null, is skipped: required fields
// are the input schema's job. A value that is neither a number nor a date fails closed.
func (pv *Provenance) Check(runID string, fields []string, input json.RawMessage) error {
	v, err := decodeNumbers(input)
	if err != nil {
		return fmt.Errorf("provenance: %w", ErrInvalidInput)
	}
	pv.mu.Lock()
	set := pv.runs[runID]
	pv.mu.Unlock()
	for _, f := range fields {
		for _, got := range resolvePointer(v, f) {
			if got == nil {
				continue
			}
			k, ok := valueKey(got)
			pv.mu.Lock()
			_, seen := set[k]
			pv.mu.Unlock()
			// rule: ARC-U-027
			// The field name is safe to report; the value is not (it may be a client amount).
			if !ok || !seen {
				return fmt.Errorf("provenance %s: %w", f, ErrUnsourcedValue)
			}
		}
	}
	return nil
}

// ReadTool wraps a read tool so every successful result an agent run receives records its
// numbers and dates into the run's set. It returns ErrBadTool when t is not a read tool.
func (pv *Provenance) ReadTool(t Tool) (Tool, error) {
	if t.Kind != registry.ToolRead || t.Handle == nil {
		return Tool{}, fmt.Errorf("read tool %q: %w", t.Name, ErrBadTool)
	}
	h := t.Handle
	// rule: ARC-I-035
	t.Handle = func(ctx context.Context, p Principal, in json.RawMessage) (ToolResult, error) {
		res, err := h(ctx, p, in)
		if err != nil {
			return ToolResult{}, err
		}
		if p.Kind == registry.PrincipalAgent {
			pv.Observe(p.RunID, res.Output)
		}
		return res, nil
	}
	return t, nil
}

// SourcedWrite wraps a write tool so, for an agent principal, every value at the declared
// JSON pointers (RFC 6901; a "*" segment means every element of an array) must be one a read
// tool returned in the same run. The check runs before the handler, so an invented amount
// or date never reaches it. A user principal (the manual path) is not checked. It returns
// ErrBadTool when t is not a write tool, no field is declared or a pointer is malformed.
func (pv *Provenance) SourcedWrite(t Tool, fields ...string) (Tool, error) {
	if t.Kind != registry.ToolWrite || t.Handle == nil || len(fields) == 0 {
		return Tool{}, fmt.Errorf("sourced write %q: %w", t.Name, ErrBadTool)
	}
	for _, f := range fields {
		if !strings.HasPrefix(f, "/") {
			return Tool{}, fmt.Errorf("sourced write %q: pointer %q: %w", t.Name, f, ErrBadTool)
		}
	}
	fields = append([]string(nil), fields...)
	h := t.Handle
	// rule: ARC-U-027
	// Agent jobs never invent money or dates: amounts and dates in an agent's write come
	// from figures the run read, so the gate it opens shows sourced values.
	t.Handle = func(ctx context.Context, p Principal, in json.RawMessage) (ToolResult, error) {
		if p.Kind == registry.PrincipalAgent {
			if err := pv.Check(p.RunID, fields, in); err != nil {
				return ToolResult{}, err
			}
		}
		return h(ctx, p, in)
	}
	return t, nil
}

func decodeNumbers(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after JSON value")
	}
	return v, nil
}

func collectValues(v any, out *[]string) { collectIn(v, false, out) }

// collectIn records v's numbers and dates. Inside an untrusted envelope (an object with
// "untrusted": true: a Quoted value, a read_document result) every "text" string is quoted
// text, and the dates and numbers it states are recorded too.
func collectIn(v any, quoted bool, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		if u, _ := x["untrusted"].(bool); u {
			quoted = true
		}
		for k, c := range x {
			if text, ok := c.(string); ok && quoted && k == "text" {
				collectQuoted(text, out)
				continue
			}
			collectIn(c, quoted, out)
		}
	case []any:
		for _, c := range x {
			collectIn(c, quoted, out)
		}
	default:
		if k, ok := valueKey(x); ok {
			*out = append(*out, k)
		}
	}
}

// collectQuoted records the dates and numbers written in quoted text. Only those values are
// kept, never the text around them.
func collectQuoted(text string, out *[]string) {
	// rule: ARC-U-027
	// A number or date a quoted source states is a value the run read, so a write built
	// from it is sourced; a figure the quote never states is not.
	for _, d := range textDate.FindAllString(text, -1) {
		*out = append(*out, "d:"+d)
	}
	for _, n := range textNumber.FindAllString(textDate.ReplaceAllString(text, " "), -1) {
		if k, ok := valueKey(json.Number(strings.ReplaceAll(n, ",", ""))); ok {
			*out = append(*out, k)
		}
	}
}

// valueKey is the set key for a number or a date. Numbers and dates live in separate key
// spaces so 20260115 never matches 2026-01-15. Integers are compared by value (5000 and
// 5000.00 match); other numbers by their literal.
func valueKey(v any) (string, bool) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return "n:" + strconv.FormatInt(i, 10), true
		}
		// A whole number written with a zero fraction ("5000.00") is the same integer.
		if whole, frac, ok := strings.Cut(x.String(), "."); ok && strings.Trim(frac, "0") == "" {
			if i, err := strconv.ParseInt(whole, 10, 64); err == nil {
				return "n:" + strconv.FormatInt(i, 10), true
			}
		}
		return "n:" + x.String(), true
	case string:
		if dateValue.MatchString(x) {
			return "d:" + x, true
		}
	}
	return "", false
}

// resolvePointer returns every value the pointer reaches; a "*" segment fans out over an
// array. Missing members resolve to nothing.
func resolvePointer(v any, ptr string) []any {
	cur := []any{v}
	for _, seg := range strings.Split(ptr, "/")[1:] {
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		var next []any
		for _, c := range cur {
			switch x := c.(type) {
			case map[string]any:
				if m, ok := x[seg]; ok {
					next = append(next, m)
				}
			case []any:
				if seg == "*" {
					next = append(next, x...)
					continue
				}
				if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(x) {
					next = append(next, x[i])
				}
			}
		}
		cur = next
	}
	return cur
}
