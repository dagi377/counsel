package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/settings"
	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// memStore is an in-memory settings.Store.
type memStore struct {
	values  map[string]string
	changes []settings.Change
}

func (m *memStore) Applied(_ context.Context, _ pgx.Tx, gateID string) (settings.Change, bool, error) {
	for _, c := range m.changes {
		if c.ApprovalID == gateID {
			return c, true, nil
		}
	}
	return settings.Change{}, false, nil
}

func (m *memStore) Apply(_ context.Context, _ pgx.Tx, c settings.Change) error {
	m.changes = append(m.changes, c)
	m.values[c.Key] = string(c.NewValue)
	return nil
}

func TestEffect(t *testing.T) {
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	payload := func(k string, v any) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"setting": k, "new_value": v})
		return b
	}
	tests := []struct {
		name      string
		payload   json.RawMessage
		approvers []registry.Principal
		repeat    bool
		want      int
		err       error
	}{
		{"terms applied", payload("payment_terms_days", 30), []registry.Principal{user("lawyer")}, false, 1, nil},
		// rule: ARC-I-012
		{"repeat is a no-op", payload("payment_terms_days", 30), []registry.Principal{user("lawyer")}, true, 1, nil},
		{"fractional rate refused", payload("interest_rate_ppm", json.RawMessage("15e-1")), []registry.Principal{user("lawyer")}, false, 0, settings.ErrInvalid},
		// rule: BIL-C-067
		{"credit window out of range refused", payload("client_credit_max_business_days", 0), []registry.Principal{user("lawyer")}, false, 0, settings.ErrInvalid},
		{"unknown setting refused", payload("colour", "red"), []registry.Principal{user("lawyer")}, false, 0, settings.ErrUnknownSetting},
		// rule: ARC-I-019
		{"agent approver refused", payload("payment_terms_days", 30), []registry.Principal{{Kind: registry.PrincipalAgent}}, false, 0, decider.ErrAgentApproves},
		// rule: ARC-I-027
		{"preparer refused", payload("payment_terms_days", 30), []registry.Principal{user("clerk")}, false, 0, decider.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memStore{values: map[string]string{}}
			e := &settings.Effect{Store: st}
			g := registry.Approved{GateID: "gate-1", Kind: settings.KindSettingsChange, Payload: tt.payload, Preparer: user("clerk"), Approvers: tt.approvers}
			err := e.Apply(context.Background(), nil, g)
			if tt.repeat && err == nil {
				err = e.Apply(context.Background(), nil, g)
			}
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			if len(st.changes) != tt.want {
				t.Fatalf("changes = %d, want %d", len(st.changes), tt.want)
			}
			if tt.want == 1 && (st.values["payment_terms_days"] != "30" || st.changes[0].ApprovedBy != "lawyer") {
				t.Fatalf("values = %v changes = %+v", st.values, st.changes)
			}
		})
	}
}
