// Package settings keeps the firm's billing settings (billing.setting, 5632). A setting
// changes only through an approved billing.settings_change gate (G-B8).
package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// KindSettingsChange is the G-B8 gate.
const KindSettingsChange = "billing.settings_change"

// Sentinel errors. Callers match them with errors.Is.
var (
	// ErrInvalid means the payload, setting or value is not valid.
	ErrInvalid = errors.New("settings: invalid setting change")
	// ErrUnknownSetting means the setting is not a billing setting.
	ErrUnknownSetting = errors.New("settings: unknown billing setting")
)

// intRange accepts a JSON integer in [lo, hi]: money in cents, rates in ppm, days.
func intRange(lo, hi int64) func(json.RawMessage) error {
	return func(v json.RawMessage) error {
		d := json.NewDecoder(bytes.NewReader(v))
		d.UseNumber()
		var num json.Number
		if err := d.Decode(&num); err != nil {
			return ErrInvalid
		}
		n, err := num.Int64()
		if err != nil || n < lo || n > hi {
			return ErrInvalid
		}
		return nil
	}
}

func boolean(v json.RawMessage) error {
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return ErrInvalid
	}
	return nil
}

// Known are the billing settings a gate may change, each with its value check. Money is
// integer cents and rates are ppm; no float is ever accepted.
var Known = map[string]func(json.RawMessage) error{
	// rule: BIL-C-067
	"client_credit_max_business_days": intRange(1, 30),
	// rule: BIL-C-052
	"auto_apply_trust_on_approval":    boolean,
	"payment_terms_days":              intRange(0, 365),
	"interest_rate_ppm":               intRange(0, 1_000_000),
	"write_off_second_approval_cents": intRange(0, 100_000_000_000),
}

// Change is one applied setting change.
type Change struct {
	Key        string
	NewValue   json.RawMessage
	ApprovalID string
	ApprovedBy string
}

// Store keeps billing settings. Every method runs in the gate tx. PGStore implements it.
type Store interface {
	// Applied returns the change recorded under gate id, or ok false.
	Applied(ctx context.Context, tx pgx.Tx, gateID string) (Change, bool, error)
	// Apply sets the value and records the change.
	Apply(ctx context.Context, tx pgx.Tx, c Change) error
}

type payload struct {
	Setting  string          `json:"setting"`
	NewValue json.RawMessage `json:"new_value"`
}

// Effect is the billing.settings_change EffectFunc: it applies the approved value.
type Effect struct {
	Store Store
}

// Apply applies the approved setting. A change this gate already applied is a no-op.
func (e *Effect) Apply(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
	by, err := decider.Check(g)
	if err != nil {
		return fmt.Errorf("settings effect: %w", err)
	}
	if e == nil || e.Store == nil {
		return fmt.Errorf("settings effect %s: not wired: %w", g.GateID, ErrInvalid)
	}
	var p payload
	if err := json.Unmarshal(g.Payload, &p); err != nil || p.Setting == "" || len(p.NewValue) == 0 {
		return fmt.Errorf("settings effect %s: payload: %w", g.GateID, ErrInvalid)
	}
	check, ok := Known[p.Setting]
	if !ok {
		return fmt.Errorf("settings effect %s: %q: %w", g.GateID, p.Setting, ErrUnknownSetting)
	}
	if err := check(p.NewValue); err != nil {
		return fmt.Errorf("settings effect %s: %s: %w", g.GateID, p.Setting, err)
	}
	// rule: ARC-I-012
	// Idempotent per gate: the change this gate applied is not applied again.
	prior, done, err := e.Store.Applied(ctx, tx, g.GateID)
	if err != nil {
		return fmt.Errorf("settings effect %s: %w", g.GateID, err)
	}
	if done {
		if prior.Key != p.Setting {
			return fmt.Errorf("settings effect %s: gate applied %s: %w", g.GateID, prior.Key, ErrInvalid)
		}
		return nil
	}
	return e.Store.Apply(ctx, tx, Change{Key: p.Setting, NewValue: p.NewValue, ApprovalID: g.GateID, ApprovedBy: by})
}

// Effects returns the effect keyed by gate kind.
func (e *Effect) Effects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{KindSettingsChange: e.Apply}
}

// PGStore is Store over billing.setting and billing.setting_change.
type PGStore struct{}

var _ Store = PGStore{}

const appliedSQL = `SELECT key, new_value FROM billing.setting_change WHERE approval_id = $1::uuid`

// applySQL records the change with the value it replaces, then sets the new value.
const applySQL = `WITH old AS (SELECT value FROM billing.setting WHERE key = $1 FOR UPDATE),
ch AS (INSERT INTO billing.setting_change (approval_id, key, old_value, new_value, approved_by)
  VALUES ($3::uuid, $1, (SELECT value FROM old), $2::jsonb, $4::uuid))
INSERT INTO billing.setting (key, value, approval_id, updated_by) VALUES ($1, $2::jsonb, $3::uuid, $4::uuid)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, approval_id = EXCLUDED.approval_id,
  updated_by = EXCLUDED.updated_by, updated_at = now()`

// Applied implements Store.
func (PGStore) Applied(ctx context.Context, tx pgx.Tx, gateID string) (Change, bool, error) {
	c := Change{ApprovalID: gateID}
	var v []byte
	err := tx.QueryRow(ctx, appliedSQL, gateID).Scan(&c.Key, &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return Change{}, false, nil
	}
	if err != nil {
		return Change{}, false, fmt.Errorf("setting change %s: %w", gateID, err)
	}
	c.NewValue = v
	return c, true, nil
}

// Apply implements Store.
func (PGStore) Apply(ctx context.Context, tx pgx.Tx, c Change) error {
	if _, err := tx.Exec(ctx, applySQL, c.Key, []byte(c.NewValue), c.ApprovalID, c.ApprovedBy); err != nil {
		return fmt.Errorf("apply setting %s: %w", c.Key, err)
	}
	return nil
}
