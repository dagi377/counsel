package gate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func noEffect(context.Context, pgx.Tx, Approved) error { return nil }

const amountSchema = `{"type":"object","required":["amount_cents"],"properties":{"amount_cents":{"type":"integer"}}}`

func validKind(slug string) GateKind {
	return GateKind{
		Slug:          slug,
		Approver:      "billing.approve",
		Risk:          RiskMedium,
		MinApprovers:  1,
		TTL:           72 * time.Hour,
		PayloadSchema: []byte(amountSchema),
		AmountPath:    "amount_cents",
		Effect:        noEffect,
	}
}

// rule: ARC-I-013
func TestARC_I_013_KindSlugIsModulePrefixed(t *testing.T) {
	tests := []struct {
		name    string
		module  string
		slug    string
		wantErr error
	}{
		{name: "module slug loads", module: "billing", slug: "billing.write_off"},
		{name: "second module loads", module: "docs", slug: "docs.purge_approval"},
		{name: "spec id rejected", module: "billing", slug: "G2", wantErr: ErrInvalidKind},
		{name: "spec id with dash rejected", module: "billing", slug: "G-B2", wantErr: ErrInvalidKind},
		{name: "prefix not the registering module", module: "docs", slug: "billing.write_off", wantErr: ErrInvalidKind},
		{name: "unknown module", module: "unknown", slug: "unknown.x", wantErr: ErrInvalidKind},
		{name: "kernel prefix", module: "core", slug: "core.x", wantErr: ErrInvalidKind},
		{name: "digits not allowed", module: "billing", slug: "billing.step2", wantErr: ErrInvalidKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ks := NewKinds("billing", "docs", "core")
			err := ks.Register(tt.module, validKind(tt.slug))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register(%q, %q) = %v, want %v", tt.module, tt.slug, err, tt.wantErr)
			}
		})
	}
}

func TestRegisterRejectsInvalidKinds(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*GateKind)
		wantErr error
	}{
		{name: "valid", mutate: func(*GateKind) {}},
		{name: "no approver", mutate: func(k *GateKind) { k.Approver = "" }, wantErr: ErrInvalidKind},
		{name: "no effect", mutate: func(k *GateKind) { k.Effect = nil }, wantErr: ErrInvalidKind},
		{name: "bad risk", mutate: func(k *GateKind) { k.Risk = "huge" }, wantErr: ErrInvalidKind},
		{name: "ttl below 1h", mutate: func(k *GateKind) { k.TTL = time.Minute }, wantErr: ErrInvalidKind},
		{name: "ttl above 720h", mutate: func(k *GateKind) { k.TTL = 721 * time.Hour }, wantErr: ErrInvalidKind},
		{name: "ttl not whole hours", mutate: func(k *GateKind) { k.TTL = 90 * time.Minute }, wantErr: ErrInvalidKind},
		{name: "negative cooling-off", mutate: func(k *GateKind) { k.CoolingOff = -time.Hour }, wantErr: ErrInvalidKind},
		{name: "no schema", mutate: func(k *GateKind) { k.PayloadSchema = nil }, wantErr: ErrInvalidKind},
		{name: "broken schema", mutate: func(k *GateKind) { k.PayloadSchema = []byte(`{"type":7}`) }, wantErr: ErrInvalidKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := validKind("billing.write_off")
			tt.mutate(&k)
			if err := NewKinds().Register("billing", k); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterDuplicate(t *testing.T) {
	e := NewPG()
	if err := e.Register(validKind("billing.write_off")); err != nil {
		t.Fatal(err)
	}
	if err := e.Register(validKind("billing.write_off")); !errors.Is(err, ErrDuplicateKind) {
		t.Fatalf("second Register = %v, want ErrDuplicateKind", err)
	}
}

// rule: ARC-I-019
func TestARC_I_019_MinApproversBounds(t *testing.T) {
	tests := []struct {
		name    string
		risk    Risk
		min     int
		wantErr error
	}{
		{name: "one approver", risk: RiskHigh, min: 1},
		{name: "three approvers", risk: RiskHigh, min: 3},
		{name: "zero approvers", risk: RiskLow, min: 0, wantErr: ErrInvalidKind},
		{name: "four approvers", risk: RiskLow, min: 4, wantErr: ErrInvalidKind},
		{name: "irreversible needs two", risk: RiskIrreversible, min: 1, wantErr: ErrInvalidKind},
		{name: "irreversible with two", risk: RiskIrreversible, min: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := validKind("billing.write_off")
			k.Risk, k.MinApprovers = tt.risk, tt.min
			if err := NewKinds().Register("billing", k); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Register = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: ARC-I-014
func TestARC_I_014_PayloadHashCanonical(t *testing.T) {
	tests := []struct {
		name  string
		a, b  string
		equal bool
	}{
		{name: "key order", a: `{"amount_cents":50000,"account_id":"L1"}`, b: `{"account_id":"L1","amount_cents":50000}`, equal: true},
		{name: "whitespace", a: `{"a":1}`, b: "{ \"a\" : 1 }\n", equal: true},
		{name: "nested key order", a: `{"x":{"b":1,"a":2}}`, b: `{"x":{"a":2,"b":1}}`, equal: true},
		{name: "html not escaped", a: `{"s":"<&>"}`, b: `{"s":"<&>"}`, equal: true},
		{name: "amount differs", a: `{"amount_cents":50000}`, b: `{"amount_cents":60000}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ha, err := PayloadHash(json.RawMessage(tt.a))
			if err != nil {
				t.Fatal(err)
			}
			hb, err := PayloadHash(json.RawMessage(tt.b))
			if err != nil {
				t.Fatal(err)
			}
			if (ha == hb) != tt.equal {
				t.Fatalf("hash equal = %v, want %v (%s vs %s)", ha == hb, tt.equal, ha, hb)
			}
		})
	}
	h, _ := PayloadHash(json.RawMessage(`{"a":1}`))
	// sha256 of the canonical bytes {"a":1}
	if want := "sha256:015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"; h != want {
		t.Fatalf("PayloadHash = %s, want %s", h, want)
	}
}

// Amounts are integer cents and rates integer ppm, so a fractional or exponent number in a
// payload is rejected before it is hashed.
// rule: ARC-I-014
func TestARC_I_014_PayloadHashRejectsFractionalNumbers(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		ok      bool
	}{
		{name: "integer", payload: `{"amount_cents":50000}`, ok: true},
		{name: "negative integer", payload: `{"amount_cents":-1}`, ok: true},
		{name: "fraction", payload: `{"amount_cents":500.5}`},
		{name: "zero fraction", payload: `{"amount_cents":500.0}`},
		{name: "exponent", payload: `{"amount_cents":5e4}`},
		{name: "nested fraction", payload: `{"lines":[{"rate_ppm":0.13}]}`},
		{name: "beyond int64", payload: `{"amount_cents":9223372036854775808}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PayloadHash(json.RawMessage(tt.payload))
			if tt.ok {
				if err != nil {
					t.Fatalf("PayloadHash = %v, want ok", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidPayload) {
				t.Fatalf("PayloadHash = %v, want ErrInvalidPayload", err)
			}
		})
	}
}

// rule: ARC-I-013
func TestARC_I_013_OpenUnknownKindIs422(t *testing.T) {
	e := NewPG()
	if err := e.Register(validKind("billing.write_off")); err != nil {
		t.Fatal(err)
	}
	if err := e.Kinds().Label("billing.write_off", "G-B2"); err != nil {
		t.Fatal(err)
	}
	user := Principal{Kind: PrincipalUser, UserID: "u1"}
	subject := Subject{Kind: "billing.bill", ID: "b1"}
	tests := []struct {
		name       string
		kind       string
		payload    string
		preparer   Principal
		wantErr    error
		wantStatus int
	}{
		{name: "unregistered kind", kind: "billing.other", payload: `{"amount_cents":1}`, preparer: user, wantErr: ErrUnknownKind, wantStatus: http.StatusUnprocessableEntity},
		{name: "spec id as kind", kind: "G-B2", payload: `{"amount_cents":1}`, preparer: user, wantErr: ErrUnknownKind, wantStatus: http.StatusUnprocessableEntity},
		{name: "payload fails schema", kind: "billing.write_off", payload: `{"amount_cents":"x"}`, preparer: user, wantErr: ErrInvalidPayload, wantStatus: http.StatusUnprocessableEntity},
		{name: "payload not object", kind: "billing.write_off", payload: `[1]`, preparer: user, wantErr: ErrInvalidPayload, wantStatus: http.StatusUnprocessableEntity},
		{name: "guest may not prepare", kind: "billing.write_off", payload: `{"amount_cents":1}`, preparer: Principal{Kind: PrincipalGuest, UserID: "g"}, wantErr: ErrNotAuthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The checks run before tx is used, so a nil tx is safe here.
			_, err := e.Open(context.Background(), nil, tt.kind, subject, json.RawMessage(tt.payload), tt.preparer)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Open = %v, want %v", err, tt.wantErr)
			}
			if tt.wantStatus != 0 {
				if p := Problem(err); p == nil || p.Status != tt.wantStatus {
					t.Fatalf("Problem(%v) = %+v, want status %d", err, p, tt.wantStatus)
				}
			}
		})
	}
}

func TestPrepareOpenAgentActsForUser(t *testing.T) {
	e := NewPG()
	if err := e.Register(validKind("billing.write_off")); err != nil {
		t.Fatal(err)
	}
	if err := e.Kinds().Label("billing.write_off", "G-B2"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		p         Principal
		wantUser  string
		wantRunID string
		wantErr   error
	}{
		{name: "user", p: Principal{Kind: PrincipalUser, UserID: "u1"}, wantUser: "u1"},
		{name: "agent", p: Principal{Kind: PrincipalAgent, RunID: "r1", ActingFor: "u2"}, wantUser: "u2", wantRunID: "r1"},
		{name: "agent without user", p: Principal{Kind: PrincipalAgent, RunID: "r1"}, wantErr: ErrNotAuthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := e.prepareOpen("billing.write_off", Subject{Kind: "s", ID: "1"}, json.RawMessage(`{"amount_cents":5}`), tt.p)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("prepareOpen = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (o.requester != tt.wantUser || o.runID != tt.wantRunID) {
				t.Fatalf("requester, run = %q, %q; want %q, %q", o.requester, o.runID, tt.wantUser, tt.wantRunID)
			}
		})
	}
}
