package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// RFC 8785 Appendix B: IEEE 754 bit patterns and their ES6 serialisation.
// rule: ARC-I-015
func TestARC_I_015_JCSNumbersAppendixB(t *testing.T) {
	tests := []struct {
		bits uint64
		want string // "" means the value has no JSON form
	}{
		{0x0000000000000000, "0"},
		{0x8000000000000000, "0"},
		{0x0000000000000001, "5e-324"},
		{0x8000000000000001, "-5e-324"},
		{0x7fefffffffffffff, "1.7976931348623157e+308"},
		{0xffefffffffffffff, "-1.7976931348623157e+308"},
		{0x4340000000000000, "9007199254740992"},
		{0xc340000000000000, "-9007199254740992"},
		{0x4430000000000000, "295147905179352830000"},
		{0x7fffffffffffffff, ""},
		{0x7ff0000000000000, ""},
		{0x44b52d02c7e14af5, "9.999999999999997e+22"},
		{0x44b52d02c7e14af6, "1e+23"},
		{0x44b52d02c7e14af7, "1.0000000000000001e+23"},
		{0x444b1ae4d6e2ef4e, "999999999999999700000"},
		{0x444b1ae4d6e2ef4f, "999999999999999900000"},
		{0x444b1ae4d6e2ef50, "1e+21"},
		{0x3eb0c6f7a0b5ed8c, "9.999999999999997e-7"},
		{0x3eb0c6f7a0b5ed8d, "0.000001"},
		{0x41b3de4355555553, "333333333.3333332"},
		{0x41b3de4355555554, "333333333.33333325"},
		{0x41b3de4355555555, "333333333.3333333"},
		{0x41b3de4355555556, "333333333.3333334"},
		{0x41b3de4355555557, "333333333.33333343"},
		{0xbecbf647612f3696, "-0.0000033333333333333333"},
		{0x43143ff3c1cb0959, "1424953923781206.2"},
	}
	for _, tt := range tests {
		got, err := es6Number(math.Float64frombits(tt.bits))
		if tt.want == "" {
			if !errors.Is(err, ErrInvalidPayload) {
				t.Errorf("%016x: err = %v, want ErrInvalidPayload", tt.bits, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%016x: = %q, %v; want %q", tt.bits, got, err, tt.want)
		}
	}
}

// RFC 8785 sec. 3.2.2 and 3.2.3 examples, plus the line and paragraph separators, which
// RFC 8785 (unlike encoding/json) writes unescaped.
// rule: ARC-I-015
func TestARC_I_015_JCSRFC8785Vectors(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "sec 3.2.2 serialisation",
			in: `{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`,
			want: `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`,
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
		{name: "minus zero", in: `[-0, 0.0, -0e10]`, want: `[0,0,0]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := decodeJSON(json.RawMessage(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			got, err := canonicalize(doc)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("canonical =\n %s\nwant\n %s", got, tt.want)
			}
		})
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
