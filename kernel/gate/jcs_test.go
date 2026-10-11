package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/canon"
)

// RFC 8785 Appendix B number vectors. Gate payload numbers are integer-only within
// +-(2^53-1), so the only Appendix B values a payload may carry are the zeros; every other
// vector (fractions, exponents, integers beyond 2^53-1) is rejected rather than formatted.
// Integers in range serialise as their decimal form, which is their ES6 form.
// rule: ARC-I-015
func TestARC_I_015_JCSNumbers(t *testing.T) {
	tests := []struct {
		in   string // JSON number literal
		want string // "" means the payload is rejected
	}{
		// Appendix B, accepted: the zeros.
		{"0", "0"},
		{"-0", "0"},
		// Appendix B, rejected: not an integer within +-(2^53-1).
		{"5e-324", ""},
		{"-5e-324", ""},
		{"1.7976931348623157e+308", ""},
		{"-1.7976931348623157e+308", ""},
		{"9007199254740992", ""},
		{"-9007199254740992", ""},
		{"295147905179352830000", ""},
		{"9.999999999999997e+22", ""},
		{"1e+23", ""},
		{"1.0000000000000001e+23", ""},
		{"999999999999999700000", ""},
		{"999999999999999900000", ""},
		{"1e+21", ""},
		{"9.999999999999997e-7", ""},
		{"0.000001", ""},
		{"333333333.3333332", ""},
		{"333333333.33333325", ""},
		{"333333333.3333333", ""},
		{"333333333.3333334", ""},
		{"333333333.33333343", ""},
		{"-0.0000033333333333333333", ""},
		{"1424953923781206.2", ""},
		// Integral but not an integer literal.
		{"0.0", ""},
		{"-0e10", ""},
		{"500.0", ""},
		{"5E4", ""},
		// Integers in range: decimal form.
		{"1", "1"},
		{"-1", "-1"},
		{"50000", "50000"},
		{"9007199254740991", "9007199254740991"},
		{"-9007199254740991", "-9007199254740991"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := JCS(json.RawMessage(`{"n":` + tt.in + `}`))
			if tt.want == "" {
				if !errors.Is(err, ErrInvalidPayload) || !errors.Is(err, canon.ErrNumber) {
					t.Fatalf("JCS(%s) = %s, %v; want ErrInvalidPayload and canon.ErrNumber", tt.in, got, err)
				}
				return
			}
			if want := `{"n":` + tt.want + `}`; err != nil || string(got) != want {
				t.Fatalf("JCS(%s) = %s, %v; want %s", tt.in, got, err, want)
			}
		})
	}
	// NaN and Infinity (Appendix B 7fffffffffffffff, 7ff0000000000000) have no JSON form.
	for _, in := range []string{`{"n":NaN}`, `{"n":Infinity}`} {
		if _, err := JCS(json.RawMessage(in)); !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("JCS(%s) err = %v, want ErrInvalidPayload", in, err)
		}
	}
}

// RFC 8785 sec. 3.2.2 and 3.2.3 examples, plus the line and paragraph separators, which
// RFC 8785 (unlike encoding/json) writes unescaped. The sec. 3.2.2 number array holds
// fractions and exponents, so it is rejected; its strings and literals are checked alone.
// rule: ARC-I-015
func TestARC_I_015_JCSRFC8785Vectors(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "sec 3.2.2 serialisation without numbers",
			in: `{
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`,
			want: `{"literals":[null,true,false],"string":"€$\u000f\nA'B\"\\\\\"/"}`,
		},
		{
			name: "sec 3.2.3 sorting",
			in: `{
  "\u20ac": "Euro Sign",
  "\r": "Carriage Return",
  "\ufb33": "Hebrew Letter Dalet With Dagesh",
  "1": "One",
  "\ud83d\ude00": "Emoji: Grinning Face",
  "\u0080": "Control",
  "\u00f6": "Latin Small Letter O With Diaeresis"
}`,
			want: "{\"\\r\":\"Carriage Return\",\"1\":\"One\",\"\u0080\":\"Control\",\"ö\":\"Latin Small Letter O With Diaeresis\",\"€\":\"Euro Sign\",\"\U0001F600\":\"Emoji: Grinning Face\",\"\uFB33\":\"Hebrew Letter Dalet With Dagesh\"}",
		},
		{name: "U+2028 and U+2029 not escaped", in: `{"s":"a\u2028b\u2029c"}`, want: "{\"s\":\"a\u2028b\u2029c\"}"},
		{name: "control short forms", in: `{"s":"\b\f\n\r\t\u0001\u001f\u007f"}`, want: "{\"s\":\"\\b\\f\\n\\r\\t\\u0001\\u001f\u007f\"}"},
		{name: "integer zeros", in: `[-0, 0]`, want: `[0,0]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JCS(json.RawMessage(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("canonical =\n %s\nwant\n %s", got, tt.want)
			}
		})
	}
	sec322 := `{"numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001]}`
	if _, err := JCS(json.RawMessage(sec322)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("JCS sec 3.2.2 numbers = %v, want ErrInvalidPayload", err)
	}
	// Gate payloads use the same serialiser: separators stay raw in the hashed bytes too.
	got, err := JCS(json.RawMessage(`{"memo":"x\u2028y","amount_cents":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"amount_cents\":1,\"memo\":\"x\u2028y\"}"; string(got) != want {
		t.Fatalf("JCS = %s, want %s", got, want)
	}
	if _, err := JCS(json.RawMessage(`{"a":1} {"b":2}`)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("JCS trailing value = %v, want ErrInvalidPayload", err)
	}
}

// A payload opened as version 1 and the same payload submitted as a revision hash alike,
// whatever its key order, whitespace or escape spelling; so an unchanged revision is not an
// edit, and a changed one gets a new hash.
// rule: ARC-I-015
func TestARC_I_015_OpenAndReviseHashAlike(t *testing.T) {
	e := NewPG()
	if err := e.Register(validKind("billing.write_off")); err != nil {
		t.Fatal(err)
	}
	if err := e.Kinds().Label("billing.write_off", "G-B2"); err != nil {
		t.Fatal(err)
	}
	preparer := Principal{Kind: PrincipalUser, UserID: "p"}
	tests := []struct {
		name, open, revise string
		changed            bool
	}{
		{name: "identical", open: `{"amount_cents":50000,"memo":"a\u2028b"}`, revise: `{"amount_cents":50000,"memo":"a\u2028b"}`},
		{name: "reordered and spaced", open: `{"amount_cents":50000,"memo":"<a&b>"}`, revise: " { \"memo\" : \"<a&b>\" ,\n \"amount_cents\" : 50000 } "},
		{name: "escape spelling", open: "{\"amount_cents\":7,\"memo\":\"\u2029\u20ac\"}", revise: `{"memo":"\u2029\u20AC","amount_cents":7}`},
		{name: "amount changed", open: `{"amount_cents":50000}`, revise: `{"amount_cents":45000}`, changed: true},
		{name: "memo changed", open: `{"amount_cents":1,"memo":"a\u2028b"}`, revise: `{"amount_cents":1,"memo":"a\u2029b"}`, changed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := e.prepareOpen("billing.write_off", Subject{Kind: "billing.bill", ID: "b1"}, json.RawMessage(tt.open), preparer)
			if err != nil {
				t.Fatal(err)
			}
			cur := Gate{ID: "g1", Kind: "billing.write_off", Version: 1, Status: StatusPending, Preparer: preparer}
			r, err := e.prepareRevise(cur, o.hash, json.RawMessage(tt.revise), preparer)
			if err != nil {
				t.Fatal(err)
			}
			if r.changed != tt.changed || bytes.Equal(r.hash, o.hash) == tt.changed {
				t.Fatalf("changed = %v (hash equal %v), want changed %v", r.changed, bytes.Equal(r.hash, o.hash), tt.changed)
			}
			want, err := PayloadHash(json.RawMessage(tt.revise))
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := PayloadHash(json.RawMessage(tt.open)); (got == want) == tt.changed {
				t.Fatalf("PayloadHash open %s vs revise %s, changed %v", got, want, tt.changed)
			}
		})
	}
}
