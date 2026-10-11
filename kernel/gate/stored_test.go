package gate

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
)

// rule: ARC-I-015
func TestARC_I_015_OpenStoresCanonicalPayload(t *testing.T) {
	e := NewPG()
	if err := e.Register(validKind("billing.write_off")); err != nil {
		t.Fatal(err)
	}
	if err := e.Kinds().Label("billing.write_off", "G-B2"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, payload, wantJCS string
	}{
		{"already canonical", `{"amount_cents":5}`, `{"amount_cents":5}`},
		{"reordered and spaced", " { \"memo\" : \"x\" ,\n \"amount_cents\" : 5 } ", `{"amount_cents":5,"memo":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := e.prepareOpen("billing.write_off", Subject{Kind: "billing.bill", ID: "b1"}, json.RawMessage(tt.payload),
				Principal{Kind: PrincipalUser, UserID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			if string(o.jcs) != tt.wantJCS {
				t.Fatalf("jcs = %s, want %s", o.jcs, tt.wantJCS)
			}
			if sum := sha256.Sum256(o.jcs); !bytes.Equal(sum[:], o.hash) {
				t.Fatal("hash is not SHA-256 of the stored bytes")
			}
		})
	}
}

// rule: ARC-I-015
func TestARC_I_015_DecideReadsStoredPayload(t *testing.T) {
	jcs := []byte(`{"amount_cents":5}`)
	sum := sha256.Sum256(jcs)
	other := sha256.Sum256([]byte(`{"amount_cents":6}`))
	tests := []struct {
		name           string
		jcs, sha, hash []byte
		wantOK         bool
		wantErr        error
	}{
		{"stored and matching", jcs, sum[:], sum[:], true, nil},
		{"row opened before payloads were stored", nil, nil, sum[:], false, nil},
		{"stored bytes differ from their hash", jcs, other[:], other[:], true, ErrPayloadChanged},
		{"stored hash differs from the version hash", jcs, sum[:], other[:], true, ErrPayloadChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok, err := storedPayload(tt.jcs, tt.sha, tt.hash)
			if ok != tt.wantOK || !errors.Is(err, tt.wantErr) {
				t.Fatalf("storedPayload = (ok %v, %v), want (ok %v, %v)", ok, err, tt.wantOK, tt.wantErr)
			}
			if tt.wantOK && tt.wantErr == nil && string(p) != string(jcs) {
				t.Fatalf("payload = %s, want %s", p, jcs)
			}
		})
	}
}

// rule: ARC-I-020
func TestARC_I_020_RecommendationIsRecorded(t *testing.T) {
	tests := []struct {
		name       string
		d          Decision
		out        verdictOut
		wantValue  string
		wantReason any
	}{
		{"approval within limit", Decision{Verdict: VerdictApprove}, verdictOut{}, "approve", nil},
		{"approval above limit", Decision{Verdict: VerdictApprove}, verdictOut{recommend: true}, "recommend", recommendReason},
		{"approval above limit with a note", Decision{Verdict: VerdictApprove, Reason: "ok by me"}, verdictOut{recommend: true}, "recommend", "ok by me"},
		{"reject keeps its reason", Decision{Verdict: VerdictReject, Reason: "no"}, verdictOut{}, "reject", "no"},
		{"self approval keeps its declared reason", Decision{Verdict: VerdictApprove, SelfApprovalReason: "sole"}, verdictOut{selfApproved: true}, "approve", "sole"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, r := decisionRow(tt.d, tt.out)
			if v != tt.wantValue || r != tt.wantReason {
				t.Fatalf("decisionRow = (%q, %v), want (%q, %v)", v, r, tt.wantValue, tt.wantReason)
			}
		})
	}
}
