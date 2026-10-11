package fixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dagi377/counsel/kernel/canon"
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

// PromptHash returns the hex sha256 of the canonical JSON (RFC 8785, kernel/canon) of p
// after masking volatile values: ULIDs, RFC 3339 timestamps, {{seed.*}} placeholders and
// the values of id, tool_use_id and run_id keys. Object keys are lower-cased with underscores removed and
// null, "", false and empty arrays/objects are dropped, so a Go struct and its protocol
// (snake_case, omitempty) encoding hash the same. Numbers use canon's literal mode: a
// decimal in a prompt hashes by its normalised digits (1.0 and 1 hash the same), with no
// float parsing.
func PromptHash(p Prompt) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("prompt hash: encode: %w", err)
	}
	v, err := canon.Decode(raw)
	if err != nil {
		return "", fmt.Errorf("prompt hash: %w", err)
	}
	v, _ = normalise(v)
	b, err := canon.MarshalLiteral(v)
	if err != nil {
		return "", fmt.Errorf("prompt hash: %w", err)
	}
	sum := sha256.Sum256(b)
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
