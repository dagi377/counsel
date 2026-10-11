package audit

import "regexp"

// Masked replaces every detail value that may not be stored.
const Masked = "[masked]"

// Kind is the shape an allowed detail value must have to be stored as is.
type Kind int

// Value kinds. Each matches only short, structured values that cannot carry free text.
const (
	// KindID is a UUID.
	KindID Kind = iota + 1
	// KindInt is a signed base-10 integer, e.g. an amount in cents or a count.
	KindInt
	// KindHash is a lowercase hex sha256 digest, e.g. a payload hash.
	KindHash
	// KindEnum is a lowercase code such as a decision or capability id ("approve", "gate.decide").
	KindEnum
)

var kindPattern = map[Kind]*regexp.Regexp{
	KindID:   regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`),
	KindInt:  regexp.MustCompile(`^-?[0-9]{1,19}$`),
	KindHash: regexp.MustCompile(`^[0-9a-f]{64}$`),
	KindEnum: regexp.MustCompile(`^[a-z][a-z_.]{0,63}$`),
}

var _ Masker = AllowMasker{}

// AllowMasker keeps only detail keys the app declared safe, and only when the value has the
// declared Kind. Every other value becomes Masked; the key stays so the row still shows that
// a field was present.
type AllowMasker struct {
	allow map[string]Kind
}

// NewAllowMasker returns a masker that keeps the keys in allow when their values match the
// given Kind. A nil or empty allow set masks every value.
func NewAllowMasker(allow map[string]Kind) AllowMasker {
	cp := make(map[string]Kind, len(allow))
	for k, v := range allow {
		cp[k] = v
	}
	return AllowMasker{allow: cp}
}

// rule: ARC-I-044
// Mask returns a copy of detail that holds only ids, integers, hashes and lowercase codes
// under declared keys. Document text, conversation text, customer names and secrets never
// match a declared Kind, so they are replaced with Masked.
func (m AllowMasker) Mask(detail map[string]string) map[string]string {
	if detail == nil {
		return nil
	}
	out := make(map[string]string, len(detail))
	for k, v := range detail {
		kind, ok := m.allow[k]
		if ok && kindPattern[kind] != nil && kindPattern[kind].MatchString(v) {
			out[k] = v
			continue
		}
		out[k] = Masked
	}
	return out
}
