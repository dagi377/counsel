package audit

import (
	"math/rand/v2"
	"strings"
	"testing"
)

var testAllow = map[string]Kind{
	"object_id":    KindID,
	"amount_after": KindInt,
	"payload_hash": KindHash,
	"decision":     KindEnum,
}

// rule: ARC-I-044
func TestARC_I_044_MaskKeepsOnlyDeclaredShapes(t *testing.T) {
	m := NewAllowMasker(testAllow)
	tests := []struct {
		name, key, value, want string
	}{
		{"id kept", "object_id", "0190a5e2-1c3b-7d4e-8f00-123456789abc", "0190a5e2-1c3b-7d4e-8f00-123456789abc"},
		{"int kept", "amount_after", "-125000", "-125000"},
		{"hash kept", "payload_hash", strings.Repeat("ab", 32), strings.Repeat("ab", 32)},
		{"enum kept", "decision", "approve", "approve"},
		{"undeclared key masked", "body", "Dear Ms Smith, please find attached", Masked},
		{"customer name under id key masked", "object_id", "Jane Smith", Masked},
		{"text under int key masked", "amount_after", "1250 paid by Jane", Masked},
		{"secret under hash key masked", "payload_hash", "sk_live_51HfQ2xKz", Masked},
		{"sentence under enum key masked", "decision", "approve because the client said so", Masked},
		{"upper-case name under enum key masked", "decision", "Smith", Masked},
		{"empty under declared key masked", "decision", "", Masked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Mask(map[string]string{tt.key: tt.value})
			if got[tt.key] != tt.want {
				t.Fatalf("Mask(%q=%q) = %q, want %q", tt.key, tt.value, got[tt.key], tt.want)
			}
		})
	}
}

func TestARC_I_044_MaskEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		allow map[string]Kind
		in    map[string]string
		want  map[string]string
	}{
		{"nil detail", testAllow, nil, nil},
		{"empty allow masks all", nil, map[string]string{"decision": "approve"}, map[string]string{"decision": Masked}},
		{"unknown kind masks", map[string]Kind{"x": Kind(99)}, map[string]string{"x": "approve"}, map[string]string{"x": Masked}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAllowMasker(tt.allow).Mask(tt.in)
			if (got == nil) != (tt.want == nil) || len(got) != len(tt.want) {
				t.Fatalf("Mask = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("Mask[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestARC_I_044_MaskDoesNotAliasInput(t *testing.T) {
	in := map[string]string{"decision": "approve"}
	out := NewAllowMasker(testAllow).Mask(in)
	out["decision"] = "changed"
	if in["decision"] != "approve" {
		t.Fatalf("Mask changed its input: %v", in)
	}
}

// secretLike builds a value that mixes upper case, digits and text, like a token, a name
// or a line of document text.
func secretLike(r *rand.Rand) string {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 _-:/+="
	prefixes := []string{"sk_live_", "Bearer ", "Jane ", "Dear ", "password=", "ghp_", ""}
	var b strings.Builder
	b.WriteString(prefixes[r.IntN(len(prefixes))])
	for range 8 + r.IntN(40) {
		b.WriteByte(alpha[r.IntN(len(alpha))])
	}
	// Every seeded secret carries an upper-case letter and a digit.
	b.WriteString("Q7")
	return b.String()
}

// rule: ARC-I-044
// rule: ARC-U-040
func TestARC_U_040_SeededSecretsNeverReachDetail(t *testing.T) {
	m := NewAllowMasker(testAllow)
	keys := []string{"object_id", "amount_after", "payload_hash", "decision", "body", "note", "token"}
	tests := []struct {
		name string
		seed uint64
	}{
		{"seed 1", 1},
		{"seed 42", 42},
		{"seed 20261011", 20261011},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewPCG(tt.seed, tt.seed))
			for range 2000 {
				detail := map[string]string{}
				var secrets []string
				for _, k := range keys {
					if r.IntN(2) == 0 {
						s := secretLike(r)
						detail[k] = s
						secrets = append(secrets, s)
					}
				}
				out := m.Mask(detail)
				for k, v := range out {
					for _, s := range secrets {
						if strings.Contains(v, s) {
							t.Fatalf("detail[%q] holds seeded secret %q", k, s)
						}
					}
				}
			}
		})
	}
}

// rule: ARC-U-040
func FuzzARC_U_040_MaskRedactsSecrets(f *testing.F) {
	for _, s := range []string{"sk_live_51HfQ2xKz", "Jane Smith", "Dear Sir, enclosed Q7", "password=Hunter2"} {
		f.Add("body", s)
		f.Add("decision", s)
	}
	m := NewAllowMasker(testAllow)
	f.Fuzz(func(t *testing.T, key, secret string) {
		hasUpper := strings.ContainsFunc(secret, func(r rune) bool { return r >= 'A' && r <= 'Z' })
		hasSpace := strings.ContainsRune(secret, ' ')
		if !hasUpper && !hasSpace {
			return // not secret-shaped for this test
		}
		out := m.Mask(map[string]string{key: secret})
		if out[key] != Masked {
			t.Fatalf("Mask(%q=%q) = %q, want %q", key, secret, out[key], Masked)
		}
	})
}
