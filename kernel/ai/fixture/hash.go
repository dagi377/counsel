package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Prompt is the part of a model request the prompt hash covers. Messages and Tools are any
// JSON-encodable values (normally the kernel/ai Message and ToolSpec slices).
type Prompt struct {
	Feature  string `json:"feature"`
	System   string `json:"system"`
	Messages any    `json:"messages"`
	Tools    any    `json:"tools"`
}

// Mask placeholders that replace volatile values before hashing.
const (
	MaskID   = "<id>"
	MaskTime = "<time>"
	MaskSeed = "<seed>"
)

var (
	// ULIDs: 26 Crockford base32 characters, first one 0-7.
	ulidRE = regexp.MustCompile(`\b[0-7][0-9A-HJKMNP-TV-Z]{25}\b`)
	// RFC 3339 date-times (a bare date is content, not volatile).
	timeRE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})?`)
	seedRE = regexp.MustCompile(`\{\{\s*seed\.[A-Za-z0-9_.]+\s*\}\}`)
)

// volatileKeys are object keys (normalised, see normKey) whose whole value is masked.
var volatileKeys = map[string]bool{"id": true, "tooluseid": true, "runid": true}

// PromptHash returns the hex sha256 of the canonical JSON (RFC 8785) of p after masking
// volatile values: ULIDs, RFC 3339 timestamps, {{seed.*}} placeholders and the values of
// id, tool_use_id and run_id keys. Object keys are lower-cased with underscores removed and
// null, "", false and empty arrays/objects are dropped, so a Go struct and its protocol
// (snake_case, omitempty) encoding hash the same.
func PromptHash(p Prompt) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("prompt hash: encode: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("prompt hash: decode: %w", err)
	}
	v, _ = normalise(v)
	var buf bytes.Buffer
	if err := canonical(&buf, v); err != nil {
		return "", fmt.Errorf("prompt hash: %w", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func normKey(k string) string {
	return strings.ToLower(strings.ReplaceAll(k, "_", ""))
}

// MaskText replaces volatile substrings of s with their placeholders.
func MaskText(s string) string {
	s = seedRE.ReplaceAllString(s, MaskSeed)
	s = timeRE.ReplaceAllString(s, MaskTime)
	return ulidRE.ReplaceAllString(s, MaskID)
}

// normalise masks and prunes v; keep is false when v is an empty value to drop.
func normalise(v any) (any, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case bool:
		return x, x
	case string:
		return MaskText(x), x != ""
	case json.Number:
		return x, true
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			n, ok := normalise(e)
			if !ok {
				// Keep positions stable inside arrays: an empty element stays as null.
				n = nil
			}
			out = append(out, n)
		}
		return out, len(out) > 0
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			nk := normKey(k)
			if volatileKeys[nk] {
				if _, ok := normalise(e); ok {
					out[nk] = MaskID
				}
				continue
			}
			if n, ok := normalise(e); ok {
				out[nk] = n
			}
		}
		return out, len(out) > 0
	}
	return v, true
}

// canonical writes v as RFC 8785 JSON: sorted keys (UTF-16 order), no whitespace,
// ECMAScript number form, minimal string escaping.
func canonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		buf.WriteString(strconv.FormatBool(x))
	case string:
		writeString(buf, x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return fmt.Errorf("number %s: %w", x, err)
		}
		s, err := esNumber(f)
		if err != nil {
			return err
		}
		buf.WriteString(s)
	case []any:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := canonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeString(buf, k)
			buf.WriteByte(':')
			if err := canonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported value %T", v)
	}
	return nil
}

func utf16Less(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		ua, ub := utf16Units(ra[i]), utf16Units(rb[i])
		for j := 0; j < len(ua) && j < len(ub); j++ {
			if ua[j] != ub[j] {
				return ua[j] < ub[j]
			}
		}
		if len(ua) != len(ub) {
			return len(ua) < len(ub)
		}
	}
	return len(ra) < len(rb)
}

func utf16Units(r rune) []uint16 {
	if r < 0x10000 {
		return []uint16{uint16(r)}
	}
	r -= 0x10000
	return []uint16{uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))}
}

func writeString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}

// esNumber formats f the way ECMAScript Number.prototype.toString does (RFC 8785 sec. 3.2.2.3).
func esNumber(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("number %v not allowed", f)
	}
	if f == 0 {
		return "0", nil
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// Shortest round-trip digits and exponent: "d.ddde±XX".
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expPart, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, err := strconv.Atoi(expPart)
	if err != nil {
		return "", fmt.Errorf("number %v: %w", f, err)
	}
	k := len(digits)
	n := exp + 1 // value = 0.digits * 10^n
	var s string
	switch {
	case k <= n && n <= 21:
		s = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		s = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		s = "0." + strings.Repeat("0", -n) + digits
	default:
		m := digits[:1]
		if k > 1 {
			m += "." + digits[1:]
		}
		es := strconv.Itoa(n - 1)
		if n-1 >= 0 {
			es = "+" + es
		}
		s = m + "e" + es
	}
	return sign + s, nil
}
