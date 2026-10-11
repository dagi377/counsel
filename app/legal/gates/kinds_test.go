package gates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

const specGatesFile = "../../../spec/gates.json"

func readSpecGates(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(specGatesFile)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryGateRegistersAsKind(t *testing.T) {
	data := readSpecGates(t)
	var raw []struct {
		Slug   string `json:"slug"`
		SpecID string `json:"spec_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	rec := &registry.Recorder{}
	cat, err := RegisterKinds(rec, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := rec.Method(registry.MethodGateKind)
	if len(calls) != len(raw) || len(raw) == 0 {
		t.Fatalf("registered %d kinds, spec/gates.json has %d", len(calls), len(raw))
	}
	registered := map[string]bool{}
	for _, c := range calls {
		registered[c.Args[0].(registry.GateKind).Slug] = true
	}
	ctx := context.Background()
	for _, g := range raw {
		t.Run(g.Slug, func(t *testing.T) {
			if !registered[g.Slug] {
				t.Fatalf("%s not registered", g.Slug)
			}
			k, err := cat.Kind(ctx, g.Slug)
			if err != nil {
				t.Fatal(err)
			}
			if k.Code != g.Slug || k.SpecGate != g.SpecID {
				t.Fatalf("kind = %q/%q, want gate_code %q, spec_gate %q", k.Code, k.SpecGate, g.Slug, g.SpecID)
			}
		})
	}
	kinds, _ := cat.Kinds(ctx)
	for i := 1; i < len(kinds); i++ {
		if kinds[i-1].Code >= kinds[i].Code {
			t.Fatalf("Kinds not ordered by Code at %d: %q >= %q", i, kinds[i-1].Code, kinds[i].Code)
		}
	}
}

func TestBillApprovalSlugAndSpecID(t *testing.T) {
	kinds, err := ParseKinds(readSpecGates(t))
	if err != nil {
		t.Fatal(err)
	}
	cat := NewCatalog(kinds)
	tests := []struct {
		code     string
		wantSpec string
		wantErr  error
	}{
		{code: "billing.bill_approval", wantSpec: "G-B2"},
		{code: "G-B2", wantErr: ErrSpecCodeAsGateCode},
		{code: "G2", wantErr: ErrSpecCodeAsGateCode},
		{code: "billing.no_such_gate", wantErr: ErrUnknownKind},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			k, err := cat.Kind(context.Background(), tt.code)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Kind(%q) = %v, want %v", tt.code, err, tt.wantErr)
			}
			if err == nil && k.SpecGate != tt.wantSpec {
				t.Fatalf("SpecGate = %q, want %q", k.SpecGate, tt.wantSpec)
			}
		})
	}
}

// CF-04: a spec id in the gate_code position is rejected at parse time.
func TestParseKindsRejectsSpecIDAsGateCode(t *testing.T) {
	const entry = `{"slug":%q,"spec_id":"G-B2","approver":"billing.approve","risk":"high","min_approvers":1,"ttl":"168h","payload_schema":{"type":"object"}%s}`
	tests := []struct {
		name    string
		slug    string
		extra   string
		wantErr error
		wantMsg string
	}{
		{name: "slug ok", slug: "billing.bill_approval"},
		{name: "spec id as code", slug: "G-B2", wantErr: ErrSpecCodeAsGateCode},
		{name: "short spec id as code", slug: "G2", wantErr: ErrSpecCodeAsGateCode},
		{name: "not a slug", slug: "Billing", wantMsg: "not <module>.<slug>"},
		{name: "dual_when parsed", slug: "billing.bill_approval", extra: `,"dual_when":"signatories>=2"`},
		{name: "bad dual_when", slug: "billing.bill_approval", extra: `,"dual_when":"owners>=2"`, wantMsg: "dual_when"},
		{name: "sole owner delay needs two approvers", slug: "billing.bill_approval", extra: `,"sole_owner_delay":"24h"`, wantMsg: "sole_owner_delay"},
		{name: "unknown field", slug: "billing.bill_approval", extra: `,"gate_code":"x"`, wantMsg: "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("[" + fmt.Sprintf(entry, tt.slug, tt.extra) + "]")
			kinds, err := ParseKinds(data)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ParseKinds = %v, want %v", err, tt.wantErr)
				}
			case tt.wantMsg != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
					t.Fatalf("ParseKinds = %v, want error containing %q", err, tt.wantMsg)
				}
			case err != nil:
				t.Fatalf("ParseKinds = %v", err)
			case strings.Contains(tt.extra, "dual_when") && kinds[0].DualApprovers != 2:
				t.Fatalf("DualApprovers = %d, want 2", kinds[0].DualApprovers)
			}
		})
	}
}

func TestSpecKindGateKind(t *testing.T) {
	kinds, err := ParseKinds(readSpecGates(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		gk := k.GateKind(nil)
		if gk.Slug != k.Code || gk.Approver != k.Approver || gk.TTL != k.TTL || gk.MinApprovers != k.MinApprovers {
			t.Fatalf("%s: GateKind = %+v does not carry the spec entry", k.Code, gk)
		}
		if !json.Valid(gk.PayloadSchema) {
			t.Fatalf("%s: payload schema is not JSON", k.Code)
		}
	}
}
