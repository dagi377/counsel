package canon

import (
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
