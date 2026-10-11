package agenttools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// TestSubmitForApproval_PayloadBuilders: submit_for_approval opens its gate with the payload
// the kind's builder read from records; a kind with no builder, or a builder that cannot
// read its record, opens nothing.
func TestSubmitForApproval_PayloadBuilders(t *testing.T) {
	builders := agenttools.NewPayloadBuilders()
	if err := builders.Register("mod.built", func(_ context.Context, s registry.Subject) (map[string]any, error) {
		return map[string]any{"amount_cents": 4200, "record_of": s.ID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := builders.Register("mod.missing", func(context.Context, registry.Subject) (map[string]any, error) {
		return nil, agenttools.ErrPayloadSource
	}); err != nil {
		t.Fatal(err)
	}
	if err := builders.Register("mod.built", func(context.Context, registry.Subject) (map[string]any, error) { return nil, nil }); !errors.Is(err, agenttools.ErrBadTool) {
		t.Fatalf("duplicate builder err = %v", err)
	}
	// rule: AI-R-005
	cases := []struct {
		name    string
		kind    string
		wantErr error
		want    map[string]any
	}{
		{"built from records", "mod.built", nil, map[string]any{"amount_cents": json.Number("4200"), "record_of": "d1", "subject_type": "draft", "subject_id": "d1"}},
		{"no builder fails closed", "mod.other", agenttools.ErrNoPayloadBuilder, nil},
		{"record missing fails closed", "mod.missing", agenttools.ErrPayloadSource, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opened json.RawMessage
			open := func(_ context.Context, _ string, _ registry.Subject, payload json.RawMessage, _ agenttools.Principal) (gate.GateID, error) {
				opened = payload
				return "g1", nil
			}
			tool, err := agenttools.SubmitForApprovalTool(open, builders)
			if err != nil {
				t.Fatal(err)
			}
			in := `{"subject_type":"draft","subject_id":"d1","gate_kind":"` + tc.kind + `","note":"pay 99999"}`
			_, err = tool.Handle(context.Background(), registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}, json.RawMessage(in))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || opened != nil {
					t.Fatalf("err = %v opened = %s", err, opened)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			dec := json.NewDecoder(bytes.NewReader(opened))
			dec.UseNumber()
			if err := dec.Decode(&got); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("payload = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("payload[%s] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

// TestARC_U_027_TriggerValuesCountAsRead: the dates and amounts of a run's trigger payload
// are recorded as read, so a period sourced from the trigger passes provenance and a
// date the trigger never stated does not.
func TestARC_U_027_TriggerValuesCountAsRead(t *testing.T) {
	// rule: ARC-U-027
	cases := []struct {
		name    string
		trigger string
		input   string
		wantErr error
	}{
		{"trigger date as period_to", `{"event":"cycle.due","period_to":"2026-09-30","item_id":"i1"}`, `{"period_to":"2026-09-30"}`, nil},
		{"date stated in trigger text", `cycle closes 2026-09-30`, `{"period_to":"2026-09-30"}`, nil},
		{"date the trigger never stated", `{"period_to":"2026-09-30"}`, `{"period_to":"2026-10-31"}`, agenttools.ErrUnsourcedValue},
		{"no trigger", ``, `{"period_to":"2026-09-30"}`, agenttools.ErrUnsourcedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pv := agenttools.NewProvenance()
			pv.ObserveTrigger("run-1", tc.trigger)
			err := pv.Check("run-1", []string{"/period_to"}, json.RawMessage(tc.input))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
