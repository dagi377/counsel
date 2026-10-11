package canon

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		name, in, want string
		err            error
	}{
		{name: "sorted keys no space", in: `{"b": 1, "a": [true, null]}`, want: `{"a":[true,null],"b":1}`},
		{name: "integers", in: `[0, -0, 100, -42, 9007199254740991, -9007199254740991]`, want: `[0,0,100,-42,9007199254740991,-9007199254740991]`},
		{name: "string escapes", in: `["a\"b\\c\n\u0001<>& é "]`, want: "[\"a\\\"b\\\\c\\n\\u0001<>& é \"]"},
		{name: "utf16 key order", in: `{"\ufb33": 2, "\ud83d\ude00": 1}`, want: "{\"\U0001F600\":1,\"\uFB33\":2}"},
		{name: "fraction", in: `[0.5]`, err: ErrNumber},
		{name: "integral fraction", in: `[1.0]`, err: ErrNumber},
		{name: "exponent", in: `[1e2]`, err: ErrNumber},
		{name: "upper exponent", in: `[1E2]`, err: ErrNumber},
		{name: "above 2^53-1", in: `[9007199254740992]`, err: ErrNumber},
		{name: "below -(2^53-1)", in: `[-9007199254740992]`, err: ErrNumber},
		{name: "beyond int64", in: `[99999999999999999999]`, err: ErrNumber},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := Decode([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Marshal(v)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("Marshal = %s, %v; want %v", got, err, tt.err)
				}
				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("Marshal = %s, %v; want %s", got, err, tt.want)
			}
		})
	}
}

func TestDecodeRejects(t *testing.T) {
	for _, in := range []string{``, `{`, `{"a":1} {"b":2}`, `NaN`} {
		if _, err := Decode([]byte(in)); !errors.Is(err, ErrSyntax) {
			t.Errorf("Decode(%q) err = %v, want ErrSyntax", in, err)
		}
	}
}

func TestMarshalRejectsNonJSONType(t *testing.T) {
	if _, err := Marshal(3); !errors.Is(err, ErrType) {
		t.Fatalf("Marshal(int) err = %v, want ErrType", err)
	}
}

func TestLiteralNumber(t *testing.T) {
	tests := []struct {
		in, want string
		err      bool
	}{
		{in: "0", want: "0"},
		{in: "-0", want: "0"},
		{in: "-0.0e5", want: "0"},
		{in: "100", want: "100"},
		{in: "1.0", want: "1"},
		{in: "1e0", want: "1"},
		{in: "10e-1", want: "1"},
		{in: "0.50", want: "0.5"},
		{in: "-12.340", want: "-12.34"},
		{in: "1e21", want: "1e+21"},
		{in: "1e20", want: "100000000000000000000"},
		{in: "123456789012345678901", want: "123456789012345678901"},
		{in: "1234567890123456789012", want: "1.234567890123456789012e+21"},
		{in: "0.000001", want: "0.000001"},
		{in: "0.0000001", want: "1e-7"},
		{in: "1.5E-7", want: "1.5e-7"},
		{in: "0.10000000000000001", want: "0.10000000000000001"},
		{in: "9007199254740993", want: "9007199254740993"},
		{in: "1e9999999999", err: true},
		{in: "01", err: true},
		{in: "1.", err: true},
		{in: "abc", err: true},
		{in: "", err: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := LiteralNumber(json.Number(tt.in))
			if tt.err {
				if !errors.Is(err, ErrNumber) {
					t.Fatalf("LiteralNumber(%q) = %q, %v; want ErrNumber", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("LiteralNumber(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

// Marshal stays integer-only for gate payloads; only MarshalLiteral takes decimals.
func TestMarshalLiteralLeavesMarshalIntegerOnly(t *testing.T) {
	v, err := Decode([]byte(`{"b":1.50,"a":[2e1,-0]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Marshal(v); !errors.Is(err, ErrNumber) {
		t.Fatalf("Marshal err = %v, want ErrNumber", err)
	}
	got, err := MarshalLiteral(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":[20,0],"b":1.5}`; string(got) != want {
		t.Fatalf("MarshalLiteral = %s, want %s", got, want)
	}
}
