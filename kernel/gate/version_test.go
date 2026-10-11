package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

// rule: ARC-I-015
func TestARC_I_015_PayloadHashIsSHA256OfJCS(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		jcs     string
	}{
		{name: "sorted keys", payload: `{"b":1,"a":2}`, jcs: `{"a":2,"b":1}`},
		{name: "whitespace dropped", payload: " { \"a\" : [ 1 , 2 ] } ", jcs: `{"a":[1,2]}`},
		{name: "literals", payload: `{"t":true,"f":false,"n":null}`, jcs: `{"f":false,"n":null,"t":true}`},
		{name: "no html escaping", payload: `{"s":"<a&b>"}`, jcs: `{"s":"<a&b>"}`},
		{name: "unicode kept", payload: `{"s":"é"}`, jcs: `{"s":"é"}`},
		// RFC 8785 sec. 3.2.3: sort by UTF-16 code units, so U+1F600 (D83D) sorts before U+FB33.
		{name: "utf16 key order", payload: `{"דּ":1,"😀":2}`, jcs: "{\"\U0001F600\":2,\"דּ\":1}"},
		{name: "nested", payload: `{"x":{"d":1,"c":[{"b":1,"a":0}]}}`, jcs: `{"x":{"c":[{"a":0,"b":1}],"d":1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JCS(json.RawMessage(tt.payload))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.jcs {
				t.Fatalf("JCS = %s, want %s", got, tt.jcs)
			}
			sum := sha256.Sum256(got)
			h, err := PayloadHash(json.RawMessage(tt.payload))
			if err != nil {
				t.Fatal(err)
			}
			if want := "sha256:" + hex.EncodeToString(sum[:]); h != want {
				t.Fatalf("PayloadHash = %s, want %s", h, want)
			}
		})
	}
	if _, err := JCS(json.RawMessage(`{"a":1.5}`)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("JCS fraction = %v, want ErrInvalidPayload", err)
	}
	if _, err := JCS(json.RawMessage(`{`)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("JCS malformed = %v, want ErrInvalidPayload", err)
	}
}

// rule: ARC-I-015
func TestARC_I_015_ReviseCreatesNewVersion(t *testing.T) {
	e := NewPG()
	k := validKind("ledger.payout")
	k.Risk, k.MinApprovers = RiskIrreversible, 2
	if err := e.Register(k); err != nil {
		t.Fatal(err)
	}
	v1 := `{"amount_cents":50000}`
	h1, err := payloadSum(json.RawMessage(v1))
	if err != nil {
		t.Fatal(err)
	}
	preparer := Principal{Kind: PrincipalUser, UserID: "p"}
	cur := Gate{ID: "g1", Kind: k.Slug, Version: 1, Status: StatusPending, Preparer: preparer}
	tests := []struct {
		name        string
		cur         Gate
		payload     string
		by          Principal
		wantErr     error
		wantChanged bool
	}{
		{name: "amount edited by preparer", cur: cur, payload: `{"amount_cents":45000}`, by: preparer, wantChanged: true},
		{name: "agent acting for preparer", cur: cur, payload: `{"amount_cents":45000}`, by: Principal{Kind: PrincipalAgent, RunID: "r", ActingFor: "p"}, wantChanged: true},
		{name: "same payload reordered is not an edit", cur: cur, payload: ` { "amount_cents" : 50000 } `, by: preparer},
		{name: "other user", cur: cur, payload: `{"amount_cents":45000}`, by: Principal{Kind: PrincipalUser, UserID: "q"}, wantErr: ErrNotPreparer},
		{name: "guest", cur: cur, payload: `{"amount_cents":45000}`, by: Principal{Kind: PrincipalGuest, UserID: "p"}, wantErr: ErrNotPreparer},
		{name: "not pending", cur: Gate{ID: "g1", Kind: k.Slug, Status: StatusApproved, Preparer: preparer}, payload: `{"amount_cents":45000}`, by: preparer, wantErr: ErrNotPending},
		{name: "fails schema", cur: cur, payload: `{"amount_cents":"x"}`, by: preparer, wantErr: ErrInvalidPayload},
		{name: "not object", cur: cur, payload: `[1]`, by: preparer, wantErr: ErrInvalidPayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := e.prepareRevise(tt.cur, h1, json.RawMessage(tt.payload), tt.by)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("prepareRevise = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if r.changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", r.changed, tt.wantChanged)
			}
			// Decisions bind to the hash they saw; a changed hash means no decision on
			// version n can count toward version n+1.
			if tt.wantChanged && string(r.hash) == string(h1) {
				t.Fatal("revised hash equals v1 hash; v1 approvals would carry over")
			}
		})
	}
}
