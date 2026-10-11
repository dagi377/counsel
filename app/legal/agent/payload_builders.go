package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateClosing is the matter closing gate kind (G13).
const GateClosing = "platform.closing"

// BlockerTrustOverdrawn is the G13 blocker an overdrawn matter carries: a matter whose trust
// ledger is below zero cannot close until the shortfall is restored (By-law 9).
const BlockerTrustOverdrawn = "trust_overdrawn"

// SubjectRecords reads the stored record of a submitted subject through its module's port,
// as field name to value (ids, dates, amounts in cents). A subject with no record is
// agenttools.ErrNotFound. Wired at boot.
type SubjectRecords interface {
	SubjectRecord(ctx context.Context, subjectType, subjectID string) (map[string]any, error)
}

// MatterTrustBalances reads a matter's signed trust balance in cents from the trust module:
// below zero when the matter's ledger is overdrawn. Wired at boot.
type MatterTrustBalances interface {
	MatterTrustBalance(ctx context.Context, matterID string) (int64, error)
}

// subjectFields are the payload keys Build adds itself; a builder never reads them.
var subjectFields = map[string]bool{"subject_type": true, "subject_id": true}

// PayloadBuilderSet returns a payload builder for every kind in kinds whose payload schema
// requires a field beyond the subject. Each builder reads the subject's record through
// records and fills the schema's fields from it; G13 closing also reads the trust balance
// from balances. Every built payload is checked against its kind's schema, so a builder that
// cannot fill one opens no gate.
func PayloadBuilderSet(kinds []gates.SpecKind, records SubjectRecords, balances MatterTrustBalances) (*agenttools.PayloadBuilders, error) {
	if records == nil {
		return nil, fmt.Errorf("payload builders: subject records: %w", ErrMissingDep)
	}
	set := agenttools.NewPayloadBuilders()
	for _, k := range kinds {
		sch, err := parseKindSchema(k.PayloadSchema)
		if err != nil {
			return nil, fmt.Errorf("payload builder %s: %w", k.Code, err)
		}
		if !sch.needsBuilder() {
			continue
		}
		valid, err := compileKindSchema(k.Code, k.PayloadSchema)
		if err != nil {
			return nil, fmt.Errorf("payload builder %s: %w", k.Code, err)
		}
		fill := recordBuilder(records, sch)
		if k.Code == GateClosing {
			if balances == nil {
				return nil, fmt.Errorf("payload builder %s: trust balances: %w", k.Code, ErrMissingDep)
			}
			fill = closingBuilder(records, balances)
		}
		if err := set.Register(k.Code, checked(k.Code, valid, fill)); err != nil {
			return nil, fmt.Errorf("payload builders: %w", err)
		}
	}
	return set, nil
}

// SpecPayloadBuilders is PayloadBuilderSet over the embedded spec/gates.json.
func SpecPayloadBuilders(records SubjectRecords, balances MatterTrustBalances) (*agenttools.PayloadBuilders, error) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		return nil, fmt.Errorf("payload builders: %w", err)
	}
	return PayloadBuilderSet(kinds, records, balances)
}

// kindSchema is the part of a payload schema a builder reads.
type kindSchema struct {
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
}

func parseKindSchema(raw json.RawMessage) (kindSchema, error) {
	var s kindSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		return kindSchema{}, fmt.Errorf("payload schema: %w", err)
	}
	return s, nil
}

// needsBuilder reports whether the schema requires a field beyond the subject ids.
func (s kindSchema) needsBuilder() bool {
	for _, f := range s.Required {
		if !subjectFields[f] {
			return true
		}
	}
	return false
}

func compileKindSchema(code string, raw json.RawMessage) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	url := "mem://payload/" + code + ".json"
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("payload schema: %w", err)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("payload schema: %w", err)
	}
	return s, nil
}

// subjectRecord reads the subject's record; a missing record fails closed.
func subjectRecord(ctx context.Context, records SubjectRecords, s registry.Subject) (map[string]any, error) {
	rec, err := records.SubjectRecord(ctx, s.Kind, s.ID)
	if err != nil {
		return nil, fmt.Errorf("subject %s: %w", s.Kind, err)
	}
	if rec == nil {
		return nil, fmt.Errorf("subject %s: %w", s.Kind, agenttools.ErrNotFound)
	}
	return rec, nil
}

// recordBuilder fills every schema field the subject's record holds. A required field the
// record lacks is agenttools.ErrPayloadSource.
func recordBuilder(records SubjectRecords, sch kindSchema) agenttools.PayloadBuilder {
	return func(ctx context.Context, s registry.Subject) (map[string]any, error) {
		rec, err := subjectRecord(ctx, records, s)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for name := range sch.Properties {
			if v, ok := rec[name]; ok && v != nil && !subjectFields[name] {
				out[name] = v
			}
		}
		for _, f := range sch.Required {
			if _, ok := out[f]; !ok && !subjectFields[f] {
				// The field name is safe to report; the value is never in an error.
				return nil, fmt.Errorf("field %s: %w", f, agenttools.ErrPayloadSource)
			}
		}
		return out, nil
	}
}

// closingBuilder fills G13 from the subject's matter and the trust module's balance. The
// balance field keeps its >= 0 meaning; an overdrawn matter carries 0 there, overdrawn,
// the shortfall and the By-law 9 blocker, so its closing gate still opens for review.
func closingBuilder(records SubjectRecords, balances MatterTrustBalances) agenttools.PayloadBuilder {
	return func(ctx context.Context, s registry.Subject) (map[string]any, error) {
		rec, err := subjectRecord(ctx, records, s)
		if err != nil {
			return nil, err
		}
		matterID, _ := rec["matter_id"].(string)
		if matterID == "" {
			return nil, fmt.Errorf("field matter_id: %w", agenttools.ErrPayloadSource)
		}
		bal, err := balances.MatterTrustBalance(ctx, matterID)
		if err != nil {
			return nil, fmt.Errorf("trust balance: %w", err)
		}
		out := map[string]any{"matter_id": matterID, "trust_balance_cents": bal, "overdrawn": false, "shortfall_cents": int64(0)}
		// rule: BIL-I-010
		// An overdrawn ledger is reported as a shortfall, never as a negative balance.
		if bal < 0 {
			out["trust_balance_cents"] = int64(0)
			out["overdrawn"] = true
			out["shortfall_cents"] = -bal
			out["blockers"] = []string{BlockerTrustOverdrawn}
		}
		return out, nil
	}
}

// checked wraps fill so its payload must pass the kind's schema.
func checked(code string, valid *jsonschema.Schema, fill agenttools.PayloadBuilder) agenttools.PayloadBuilder {
	return func(ctx context.Context, s registry.Subject) (map[string]any, error) {
		out, err := fill(ctx, s)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("payload: %w", err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("payload: %w", err)
		}
		if err := valid.Validate(v); err != nil {
			// The validator's message may quote a value, so only the kind is reported.
			return nil, fmt.Errorf("payload %s does not match its schema: %w", code, agenttools.ErrPayloadSource)
		}
		return out, nil
	}
}
