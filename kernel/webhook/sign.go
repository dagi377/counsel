package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Tolerance is the largest allowed distance between webhook-timestamp and now.
const Tolerance = 5 * time.Minute

// signaturePrefix marks a Standard Webhooks v1 (HMAC-SHA256) signature.
const signaturePrefix = "v1,"

var _ Signer = HMACSigner{}

// HMACSigner implements Signer with Standard Webhooks v1 signatures.
type HMACSigner struct{}

// Sign returns "v1,<base64 HMAC-SHA256(secret, msgID.unixTS.body)>".
//
// rule: ARC-I-033
func (HMACSigner) Sign(secret []byte, msgID string, ts time.Time, body []byte) string {
	return signaturePrefix + base64.StdEncoding.EncodeToString(mac(secret, msgID, ts, body))
}

// Verify accepts header when ts is within Tolerance of now and any space-separated v1
// signature matches. Several signatures appear while a secret rotates. Errors never carry
// the secret or a signature value.
func (HMACSigner) Verify(secret []byte, msgID string, ts, now time.Time, body []byte, header string) error {
	// rule: ARC-I-033
	if d := now.Sub(ts); d > Tolerance || d < -Tolerance {
		return fmt.Errorf("verify %s: %w", msgID, ErrTimestampSkew)
	}
	want := mac(secret, msgID, ts, body)
	// rule: ARC-I-033
	for _, s := range strings.Fields(header) {
		enc, ok := strings.CutPrefix(s, signaturePrefix)
		if !ok {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			continue
		}
		if hmac.Equal(got, want) {
			return nil
		}
	}
	return fmt.Errorf("verify %s: %w", msgID, ErrBadSignature)
}

// Headers returns the Standard Webhooks headers for one delivery attempt of m. Every key in
// secrets adds one signature, so both keys sign during a rotation overlap. The webhook-id is
// m.ID on every attempt, so a receiver dedups at-least-once retries on it.
func Headers(s Signer, secrets [][]byte, m Message, body []byte) http.Header {
	h := http.Header{}
	// rule: ARC-I-034
	h.Set(HeaderID, m.ID)
	h.Set(HeaderTimestamp, strconv.FormatInt(m.Timestamp.Unix(), 10))
	sigs := make([]string, 0, len(secrets))
	for _, k := range secrets {
		sigs = append(sigs, s.Sign(k, m.ID, m.Timestamp, body))
	}
	// rule: ARC-I-033
	h.Set(HeaderSignature, strings.Join(sigs, " "))
	return h
}

func mac(secret []byte, msgID string, ts time.Time, body []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(msgID + "." + strconv.FormatInt(ts.Unix(), 10) + "."))
	m.Write(body)
	return m.Sum(nil)
}
