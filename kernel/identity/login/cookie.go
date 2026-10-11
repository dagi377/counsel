package login

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// signer signs and opens cookie values: base64url(payload) "." base64url(HMAC-SHA256).
// Each use carries its own purpose label, so a value signed for one cookie never opens as
// another.
type signer struct{ key []byte }

func (s signer) mac(purpose, payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write([]byte(payload))
	return m.Sum(nil)
}

func (s signer) seal(purpose string, fields ...string) string {
	payload := strings.Join(fields, "|")
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(s.mac(purpose, payload))
}

// open returns the fields of a value sealed for purpose, or false when it does not verify
// or has a different field count.
func (s signer) open(purpose, value string, n int) ([]string, bool) {
	p, sig, ok := strings.Cut(value, ".")
	if !ok {
		return nil, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(p)
	if err != nil {
		return nil, false
	}
	got, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac(purpose, string(payload))) {
		return nil, false
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != n {
		return nil, false
	}
	return fields, true
}
