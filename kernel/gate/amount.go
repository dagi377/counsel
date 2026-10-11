package gate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/dagi377/counsel/kernel/registry"
)

// PreparersFunc is a kind's editor lookup for the preparer set (registry.GateKind.Preparers).
type PreparersFunc = registry.PreparersFunc

// centsSuffix marks a payload property that holds money in integer cents.
const centsSuffix = "_cents"

// moneyFields returns every "*_cents" property the payload schema declares, at any depth,
// sorted. A kind with any of them carries money.
func moneyFields(schema []byte) []string {
	var doc any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if props, ok := t["properties"].(map[string]any); ok {
				for name := range props {
					if strings.HasSuffix(name, centsSuffix) {
						out = append(out, name)
					}
				}
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(doc)
	slices.Sort(out)
	return slices.Compact(out)
}

// schemaDeclares reports whether the schema declares the property at dot path.
func schemaDeclares(schema []byte, path string) bool {
	var node map[string]any
	if err := json.Unmarshal(schema, &node); err != nil {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		props, ok := node["properties"].(map[string]any)
		if !ok {
			return false
		}
		if node, ok = props[part].(map[string]any); !ok {
			return false
		}
	}
	return true
}

// checkAmountPath validates a kind's money declaration: a kind that carries money names
// the field its amount is read from, and a capped kind always has one.
func checkAmountPath(k GateKind) error {
	money := moneyFields(k.PayloadSchema)
	switch {
	case k.AmountPath == "" && len(money) > 0:
		return fmt.Errorf("payload carries money (%s) but the kind declares no amount path", strings.Join(money, ", "))
	case k.AmountPath == "" && k.LimitKind != "":
		return fmt.Errorf("authority limit %q needs an amount path", k.LimitKind)
	case k.AmountPath != "" && !schemaDeclares(k.PayloadSchema, k.AmountPath):
		return fmt.Errorf("amount path %q is not a payload schema property", k.AmountPath)
	}
	return nil
}

// payloadAmount returns the integer cents at dot path in payload, or nil when absent.
func payloadAmount(payload json.RawMessage, path string) (*int64, error) {
	raw := json.RawMessage(payload)
	for _, part := range strings.Split(path, ".") {
		var doc map[string]json.RawMessage
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Errorf("payload %s: %w", path, ErrInvalidPayload)
		}
		next, ok := doc[part]
		if !ok || string(next) == "null" {
			return nil, nil
		}
		raw = next
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("payload %s: %w", path, ErrInvalidPayload)
	}
	n, ok := v.(json.Number)
	if !ok {
		return nil, fmt.Errorf("payload %s: not a number: %w", path, ErrInvalidPayload)
	}
	c, err := n.Int64()
	if err != nil {
		return nil, fmt.Errorf("payload %s: %w", path, ErrInvalidPayload)
	}
	return &c, nil
}

// limitInputs returns the authority-limit kind and the payload amount one decision on a
// gate of kind k is checked against. It runs on every Decide and fails closed: a kind that
// carries money or has a limit must declare its amount path, and a capped approval needs
// its payload and amount. lk is empty when the decision is uncapped.
func limitInputs(k GateKind, h DecideHooks, payload json.RawMessage, v Verdict) (lk string, amount *int64, err error) {
	// rule: ARC-I-020
	if err := checkAmountPath(k); err != nil {
		return "", nil, fmt.Errorf("kind %q: %w: %w", k.Slug, ErrInvalidKind, err)
	}
	if v != VerdictApprove || k.AmountPath == "" {
		return "", nil, nil
	}
	lk = k.LimitKind
	if lk == "" {
		lk = h.LimitKinds[k.Slug]
	}
	if payload == nil {
		return "", nil, fmt.Errorf("kind %q carries money but the gate has no payload: %w", k.Slug, ErrInvalidPayload)
	}
	if amount, err = payloadAmount(payload, k.AmountPath); err != nil {
		return "", nil, err
	}
	if amount == nil && lk != "" {
		return "", nil, fmt.Errorf("kind %q: payload has no %s: %w", k.Slug, k.AmountPath, ErrInvalidPayload)
	}
	return lk, amount, nil
}
