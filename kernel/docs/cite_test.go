package docs

import (
	"errors"
	"testing"
)

// rule: ARC-I-037
func TestNormalizeQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Limitation  period\n expires", "Limitation period expires"},
		{"  a\t\tb  ", "a b"},
		{"café", "café"}, // NFD -> NFC
		{"", ""},
	}
	for _, tc := range tests {
		if got := NormalizeQuote(tc.in); got != tc.want {
			t.Errorf("NormalizeQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if QuoteHash("Limitation  period\n expires") != QuoteHash("Limitation period expires") {
		t.Error("hash differs for equivalent quotes")
	}
	if h := QuoteHash("x"); len(h) != len(QuotePrefix)+64 || h[:len(QuotePrefix)] != QuotePrefix {
		t.Errorf("QuoteHash format = %q", h)
	}
}

func TestParseText(t *testing.T) {
	txt := ParseText("v1", []byte("p1 l1\r\np1 l2\n\fcafé\n"))
	if len(txt.Pages) != 2 || len(txt.Pages[0]) != 2 || len(txt.Pages[1]) != 1 {
		t.Fatalf("pages = %+v", txt.Pages)
	}
	if l := txt.Pages[1][0]; l.Page != 2 || l.No != 1 || l.Text != "café" {
		t.Errorf("line = %+v, want NFC page 2 line 1", l)
	}
	if _, err := txt.Page(3); !errors.Is(err, ErrBadRange) {
		t.Errorf("Page(3) err = %v, want ErrBadRange", err)
	}
}

// rule: ARC-I-037
func TestCiteVerify(t *testing.T) {
	src := []byte("intro\n\fa\nLimitation  period\n expires\nend")
	v1 := ParseText("v1", src)
	c, err := v1.Cite(2, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if c.QuoteHash != QuoteHash("Limitation period expires") {
		t.Errorf("QuoteHash = %s", c.QuoteHash)
	}
	changed := ParseText("v1", []byte("intro\n\fa\nLimitation period\n has expired\nend"))
	short := ParseText("v1", []byte("intro\n\fa"))
	reflowed := ParseText("v1", []byte("intro\n\fa\nLimitation period\n\texpires\nend"))
	tests := []struct {
		name string
		text Text
		cite Citation
		want error
	}{
		{"same version", v1, c, nil},
		{"whitespace-only change", reflowed, c, nil},
		{"changed text", changed, c, ErrCitationMismatch},
		{"lines gone", short, c, ErrCitationMismatch},
		{"other version", ParseText("v2", src), c, ErrCitationMismatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.text.Verify(tc.cite)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("Verify = %v, want %v", err, tc.want)
			}
		})
	}
	for _, r := range [][3]int{{0, 1, 1}, {3, 1, 1}, {2, 0, 1}, {2, 3, 2}, {2, 1, 5}} {
		if _, err := v1.Cite(r[0], r[1], r[2]); !errors.Is(err, ErrBadRange) {
			t.Errorf("Cite%v err = %v, want ErrBadRange", r, err)
		}
	}
}
